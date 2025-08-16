// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import {Script} from "forge-std/Script.sol";
import {Raffle} from "src/Raffle.sol";
import {HelperConfig} from "script/HelperConfig.s.sol";
import {CreateSubscription, FundSubscription, AddConsumer} from "script/Interactions.s.sol";

contract DeployRaffle is Script {
    function run () public {
        deployContract();
    }

    function deployContract() public returns (Raffle, HelperConfig) {
        HelperConfig helperConfig = new HelperConfig();
        // local -> deploy mocks, get local config
        // sepolia -> get sepolia config
        HelperConfig.NetworkConfig memory config = helperConfig.getConfig();

        if (config.subscriptionId == 0) {
            // create subscription
            CreateSubscription createSubscription = new CreateSubscription();
            (config.subscriptionId, config._vrfCoordinator) = 
                createSubscription.createSubscription(config._vrfCoordinator, config.account);

            // fund it!
            FundSubscription fundSubscription = new FundSubscription();
            fundSubscription.fundSubscription(config._vrfCoordinator, config.subscriptionId, config.link, config.account);

        }

        vm.startBroadcast(config.account);
        Raffle raffle = new Raffle(
            config.entranceFee,
            config.interval,
            config._vrfCoordinator,
            config.gasLane,
            config.subscriptionId,
            config.callbackGasLimit
        );
        vm.stopBroadcast();

        AddConsumer addConsumer = new AddConsumer();
        // don't need to broadcast because we already did that in the addConsumer contract
        addConsumer.addConsumer(address(raffle), config._vrfCoordinator, config.subscriptionId, config.account);

        return (raffle, helperConfig);
    }
}