// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import {PermissionedRwaToken} from "../src/PermissionedRwaToken.sol";

interface Vm {
    function prank(address sender) external;
    function expectRevert() external;
}

contract PermissionedRwaTokenTest {
    Vm private constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    address private constant ADMIN = address(0xA11CE);
    address private constant VAULT = address(0xBEEF);
    address private constant USER = address(0xCAFE);
    address private constant OUTSIDER = address(0xBAD);

    PermissionedRwaToken private token;

    function setUp() public {
        vm.prank(ADMIN);
        token = new PermissionedRwaToken("Cytisus AAPL RWA (Simulation)", "cAAPL", ADMIN, VAULT);
        vm.prank(ADMIN);
        token.setPermission(USER, true);
    }

    function testMintUsesWholeZeroDecimalUnitsAndTracksOperation() public {
        bytes32 operationId = keccak256("mint-1");
        vm.prank(ADMIN);
        token.mint(operationId, VAULT, 3);
        require(token.decimals() == 0, "decimals");
        require(token.totalSupply() == 3, "supply");
        require(token.balanceOf(VAULT) == 3, "vault balance");
        require(token.operationExecuted(operationId), "operation");
    }

    function testDuplicateOperationCannotMintTwice() public {
        bytes32 operationId = keccak256("duplicate");
        vm.prank(ADMIN);
        token.mint(operationId, VAULT, 1);
        vm.expectRevert();
        vm.prank(ADMIN);
        token.mint(operationId, VAULT, 1);
        require(token.totalSupply() == 1, "duplicate mint");
    }

    function testWhitelistTransferAndRejectUnapprovedRecipient() public {
        vm.prank(ADMIN);
        token.mint(keccak256("mint-transfer"), VAULT, 2);
        vm.prank(VAULT);
        token.transfer(USER, 1);
        require(token.balanceOf(USER) == 1, "user balance");
        vm.expectRevert();
        vm.prank(USER);
        token.transfer(OUTSIDER, 1);
    }

    function testFrozenAndPausedTransfersAreRejected() public {
        vm.prank(ADMIN);
        token.mint(keccak256("mint-freeze"), VAULT, 2);
        vm.prank(ADMIN);
        token.setFrozen(USER, true);
        vm.expectRevert();
        vm.prank(VAULT);
        token.transfer(USER, 1);
        vm.prank(ADMIN);
        token.setFrozen(USER, false);
        vm.prank(ADMIN);
        token.setPaused(true);
        vm.expectRevert();
        vm.prank(VAULT);
        token.transfer(USER, 1);
    }

    function testBurnAndForcedRedemptionReduceSupply() public {
        vm.prank(ADMIN);
        token.mint(keccak256("mint-burn"), VAULT, 3);
        vm.prank(ADMIN);
        token.burn(keccak256("burn"), VAULT, 1);
        vm.prank(ADMIN);
        token.setPaused(true);
        vm.prank(ADMIN);
        token.forcedRedemption(keccak256("forced"), VAULT, 1);
        require(token.totalSupply() == 1, "burn supply");
        require(token.balanceOf(VAULT) == 1, "burn balance");
    }

    function testOnlyAdminCanControlPolicyOrSupply() public {
        vm.expectRevert();
        vm.prank(USER);
        token.setPaused(true);
        vm.expectRevert();
        vm.prank(USER);
        token.mint(keccak256("unauthorized"), USER, 1);
    }
}
