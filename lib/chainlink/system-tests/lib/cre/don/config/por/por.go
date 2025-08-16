package por

import (
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"

	libc "github.com/smartcontractkit/chainlink/system-tests/lib/conversions"
	"github.com/smartcontractkit/chainlink/system-tests/lib/cre/don"
	"github.com/smartcontractkit/chainlink/system-tests/lib/cre/don/config"
	"github.com/smartcontractkit/chainlink/system-tests/lib/cre/don/node"
	keystoneflags "github.com/smartcontractkit/chainlink/system-tests/lib/cre/flags"
	cretypes "github.com/smartcontractkit/chainlink/system-tests/lib/cre/types"
)

func GenerateConfigs(input cretypes.GeneratePoRConfigsInput) (cretypes.NodeIndexToConfigOverride, error) {
	if err := input.Validate(); err != nil {
		return nil, errors.Wrap(err, "input validation failed")
	}
	configOverrides := make(cretypes.NodeIndexToConfigOverride)

	// if it's only a gateway DON, we don't need to generate any extra configuration, the default one will do
	if keystoneflags.HasFlag(input.Flags, cretypes.GatewayDON) && (!keystoneflags.HasFlag(input.Flags, cretypes.WorkflowDON) && !keystoneflags.HasFlag(input.Flags, cretypes.CapabilitiesDON)) {
		return configOverrides, nil
	}

	chainIDInt, err := strconv.Atoi(input.BlockchainOutput.ChainID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert chain ID to int")
	}
	chainIDUint64 := libc.MustSafeUint64(int64(chainIDInt))

	// find bootstrap node for the Don
	var donBootstrapNodeHost string
	var donBootstrapNodePeerID string

	bootstrapNodes, err := node.FindManyWithLabel(input.DonMetadata.NodesMetadata, &cretypes.Label{Key: node.NodeTypeKey, Value: cretypes.BootstrapNode}, node.EqualLabels)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find bootstrap nodes")
	}

	switch len(bootstrapNodes) {
	case 0:
		// if DON doesn't have bootstrap node, we need to use the global bootstrap node
		donBootstrapNodeHost = input.PeeringData.GlobalBootstraperHost
		donBootstrapNodePeerID = input.PeeringData.GlobalBootstraperPeerID
	case 1:
		bootstrapNode := bootstrapNodes[0]

		donBootstrapNodePeerID, err = node.ToP2PID(bootstrapNode, node.KeyExtractingTransformFn)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get bootstrap node peer ID")
		}

		for _, label := range bootstrapNode.Labels {
			if label.Key == node.HostLabelKey {
				donBootstrapNodeHost = label.Value
				break
			}
		}

		if donBootstrapNodeHost == "" {
			return nil, errors.New("failed to get bootstrap node host from labels")
		}

		var nodeIndex int
		for _, label := range bootstrapNode.Labels {
			if label.Key == node.IndexKey {
				nodeIndex, err = strconv.Atoi(label.Value)
				if err != nil {
					return nil, errors.Wrap(err, "failed to convert node index to int")
				}
				break
			}
		}

		// generate configuration for the bootstrap node
		configOverrides[nodeIndex] = config.BootstrapEVM(donBootstrapNodePeerID, chainIDUint64, input.CapabilitiesRegistryAddress, input.BlockchainOutput.Nodes[0].InternalHTTPUrl, input.BlockchainOutput.Nodes[0].InternalWSUrl)

		if keystoneflags.HasFlag(input.Flags, cretypes.WorkflowDON) {
			configOverrides[nodeIndex] += config.BoostrapDon2DonPeering(input.PeeringData)
		}
	default:
		return nil, errors.New("multiple bootstrap nodes within a DON found, expected only one")
	}

	// find worker nodes
	workflowNodeSet, err := node.FindManyWithLabel(input.DonMetadata.NodesMetadata, &cretypes.Label{Key: node.NodeTypeKey, Value: cretypes.WorkerNode}, node.EqualLabels)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find worker nodes")
	}

	for i := range workflowNodeSet {
		var nodeIndex int
		for _, label := range workflowNodeSet[i].Labels {
			if label.Key == node.IndexKey {
				nodeIndex, err = strconv.Atoi(label.Value)
				if err != nil {
					return nil, errors.Wrap(err, "failed to convert node index to int")
				}
			}
		}

		// for now we just assume that every worker node is connected to one EVM chain
		configOverrides[nodeIndex] = config.WorkerEVM(donBootstrapNodePeerID, donBootstrapNodeHost, input.PeeringData, chainIDUint64, input.CapabilitiesRegistryAddress, input.BlockchainOutput.Nodes[0].InternalHTTPUrl, input.BlockchainOutput.Nodes[0].InternalWSUrl)
		var nodeEthAddr common.Address
		for _, label := range workflowNodeSet[i].Labels {
			if label.Key == node.EthAddressKey {
				if label.Value == "" {
					return nil, errors.New("eth address label value is empty")
				}
				nodeEthAddr = common.HexToAddress(label.Value)
				break
			}
		}

		if keystoneflags.HasFlag(input.Flags, cretypes.WriteEVMCapability) {
			configOverrides[nodeIndex] += config.WorkerWriteEMV(
				nodeEthAddr,
				input.ForwarderAddress,
			)
		}

		// if it's workflow DON configure workflow registry, unless there's no gateway connector data
		// which means that the workflow DON is using only workflow jobs and won't be downloading any WASM-compiled workflows
		if keystoneflags.HasFlag(input.Flags, cretypes.WorkflowDON) && input.GatewayConnectorOutput != nil {
			configOverrides[nodeIndex] += config.WorkerWorkflowRegistry(
				input.WorkflowRegistryAddress, chainIDUint64)
		}

		// workflow DON nodes might need gateway connector to download WASM workflow binaries,
		// but if the workflowDON is using only workflow jobs, we don't need to set the gateway connector
		// gateway is also required by various capabilities
		if (keystoneflags.HasFlag(input.Flags, cretypes.WorkflowDON) && input.GatewayConnectorOutput != nil) || don.NodeNeedsGateway(input.Flags) {
			configOverrides[nodeIndex] += config.WorkerGateway(
				nodeEthAddr,
				chainIDUint64,
				input.DonID,
				*input.GatewayConnectorOutput,
			)
		}
	}

	return configOverrides, nil
}
