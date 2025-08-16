package deployment

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/smartcontractkit/ccip-owner-contracts/pkg/proposal/timelock"
	"github.com/smartcontractkit/mcms"

	"github.com/smartcontractkit/chainlink/deployment/operations"
)

var (
	ErrInvalidConfig      = errors.New("invalid changeset config")
	ErrInvalidEnvironment = errors.New("invalid environment")
)

// ChangeSet is equivalent to ChangeLogic
// Deprecated: use the ChangeLogic type, or an instance of a ChangeSetV2 in infrastructure or validation code.
type ChangeSet[C any] func(e Environment, config C) (ChangesetOutput, error)

// ChangeLogic encapsulates the active behavior of a ChangeSetV2.
// The config struct contains environment-specific inputs for this logical change. For example, it might contain
// the chainSelectors against which this change logic should be applied, or certain contract addresses or configuration
// values to be used in this change.
// The function should perform any deployment or configuration tasks, compose and propose job-specs, and generate any
// MCMS proposals necessary.
// This is the standalone version of ChangeSetV2.Apply for use with CreateChangeSet
//
// ChangeLogic functions should operate on a modest number of chains to reduce the risk of partial failures.
type ChangeLogic[C any] func(e Environment, config C) (ChangesetOutput, error)

// PreconditionVerifier functions should evaluate the supplied config, in the context of an environment, to ensure that
// the config struct is correct, and that the environmental preconditions are as expected. This is the standalone
// version of ChangeSetV2.VerifyPreconditions for use with CreateChangeSet
//
// If the configuration is unexpected type or format, the changeset should return ErrInvalidConfig. If there are
// surprising aspects in the environment (a contract expected to be present cannot be located, etc.), then
// ErrInvalidEnvironment should be returned.
type PreconditionVerifier[C any] func(e Environment, config C) error

// ChangeSetV2 is a type which encapsulates the logic to perform a set of changes to be made to an environment, in the
// context of deploying Chainlink's product operations - namely deploying and configuring contracts, generating and
// proposing TOML job-specs and interacting with the Job Distributor, and creating MCMS proposals.
//
// ChangeSetV2 has a pre-validation function which is optional (can be implemented as a no-op), which execution
// environments (such as the migrations infrastructure in chainlink-deployments) should execute before invoking the
// Apply method.
//
// > Note: ChangeSetV2 replaces ChangeSet, though its Apply method is identical in signature to a ChangeSet function.
type ChangeSetV2[C any] interface {
	// Apply performs the logic of the changeset, including any side effects, such as on-chain (non-MCMS) writes or
	// contract deployments,  job-spec creation and Job-Distributor interaction, MCMS proposal creation, etc. It should
	// return the ingredients of the side effects in a ChangesetOutput.
	Apply(e Environment, config C) (ChangesetOutput, error)

	// VerifyPreconditions function verifies the preconditions of the config. It should have no side effects, instead
	// returning an error if the ChangeSetV2 should not be applied, or nil if the ChangeSetV2 is safe to apply.
	VerifyPreconditions(e Environment, config C) error
}

type simpleChangeSet[C any] struct {
	apply  ChangeLogic[C]
	verify PreconditionVerifier[C]
}

func (scs simpleChangeSet[C]) Apply(e Environment, config C) (ChangesetOutput, error) {
	return scs.apply(e, config)
}

func (scs simpleChangeSet[C]) VerifyPreconditions(e Environment, config C) error {
	return scs.verify(e, config)
}

// CreateChangeSet creates a ChangeSetV2 from an existing execution function (or an older ChangeSet) and a
// precondition verification function.
func CreateChangeSet[C any](applyFunc ChangeLogic[C], verifyFunc func(e Environment, config C) error) ChangeSetV2[C] {
	return simpleChangeSet[C]{
		apply:  applyFunc,
		verify: verifyFunc,
	}
}

func CreateLegacyChangeSet[C any](changeset ChangeSet[C]) ChangeSetV2[C] {
	var cs ChangeLogic[C] = func(e Environment, config C) (ChangesetOutput, error) { return changeset(e, config) }
	return simpleChangeSet[C]{
		apply:  cs,
		verify: func(e Environment, config C) error { return nil },
	}
}

// ProposedJob represents a job spec which has been proposed to a node, with the JobID returned by the
// Job Distributor.
type ProposedJob struct {
	JobID string
	Node  string
	Spec  string
}

// ChangesetOutput is the output of a Changeset function.
// Think of it like a state transition output.
// The address book here should contain only new addresses created in
// this changeset.
type ChangesetOutput struct {
	// Deprecated: Prefer Jobs instead.
	JobSpecs map[string][]string `deprecated:"true"`
	Jobs     []ProposedJob
	// Deprecated: Prefer MCMSTimelockProposals instead, will be removed in future
	Proposals                  []timelock.MCMSWithTimelockProposal
	MCMSTimelockProposals      []mcms.TimelockProposal
	DescribedTimelockProposals []string
	MCMSProposals              []mcms.Proposal
	AddressBook                AddressBook
	// Reports are populated by the Operations API with the
	// results of the operations executed in the changeset.
	Reports []operations.Report[any, any]
}

// ViewState produces a product specific JSON representation of
// the on and offchain state of the environment.
type ViewState func(e Environment) (json.Marshaler, error)

type ViewStateV2 func(e Environment, previousView json.Marshaler) (json.Marshaler, error)

// MergeChangesetOutput merges the source ChangesetOutput into the destination ChangesetOutput.
// It is useful to combine multiple ChangesetOutput objects into one to create one consolidated changeset from multiple granular changesets.
// Ensure to run proposalutils.AggregateProposals at the end of consolidated changeset to ensure the proposals are merged correctly.
func MergeChangesetOutput(env Environment, dest *ChangesetOutput, src ChangesetOutput) error {
	if dest == nil {
		return nil
	}

	if dest.AddressBook == nil {
		dest.AddressBook = src.AddressBook
	} else if src.AddressBook != nil {
		if err := dest.AddressBook.Merge(src.AddressBook); err != nil {
			return fmt.Errorf("failed to merge address book: %w", err)
		}
		if err := env.ExistingAddresses.Merge(src.AddressBook); err != nil {
			return fmt.Errorf("failed to merge existing addresses to environment: %w", err)
		}
	}
	if dest.Jobs == nil {
		dest.Jobs = src.Jobs
	} else if src.Jobs != nil {
		dest.Jobs = append(dest.Jobs, src.Jobs...)
	}
	if dest.MCMSTimelockProposals == nil {
		dest.MCMSTimelockProposals = src.MCMSTimelockProposals
	} else if src.MCMSTimelockProposals != nil {
		dest.MCMSTimelockProposals = append(dest.MCMSTimelockProposals, src.MCMSTimelockProposals...)
	}
	if dest.DescribedTimelockProposals == nil {
		dest.DescribedTimelockProposals = src.DescribedTimelockProposals
	} else if src.DescribedTimelockProposals != nil {
		dest.DescribedTimelockProposals = append(dest.DescribedTimelockProposals, src.DescribedTimelockProposals...)
	}

	if dest.MCMSProposals == nil {
		dest.MCMSProposals = src.MCMSProposals
	} else if src.MCMSProposals != nil {
		dest.MCMSProposals = append(dest.MCMSProposals, src.MCMSProposals...)
	}

	return nil
}
