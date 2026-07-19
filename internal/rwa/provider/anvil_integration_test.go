//go:build integration

package provider

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

func TestAnvilAdapterLiveContract(t *testing.T) {
	rpcURL := os.Getenv("RWA_ANVIL_TEST_RPC_URL")
	contractAddress := os.Getenv("RWA_ANVIL_TEST_CONTRACT_ADDRESS")
	if rpcURL == "" || contractAddress == "" {
		t.Skip("RWA_ANVIL_TEST_RPC_URL and RWA_ANVIL_TEST_CONTRACT_ADDRESS are required")
	}
	adapter, err := NewAnvilAdapter(config.EnvironmentTest, AnvilConfig{
		RPCURL: rpcURL, AdminAddress: "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	health, err := adapter.Health(ctx)
	if err != nil || health.Status != "UP" || health.ChainID != BaseAnvilChainID {
		t.Fatalf("unexpected Anvil health: %+v err=%v", health, err)
	}

	vault := "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"
	mintOperation := strings.Repeat("1", 64)
	request := OperationRequest{ContractAddress: contractAddress, OperationID: mintOperation, Account: vault, Quantity: money.MustParse("2")}
	if result, err := adapter.Mint(ctx, request); err != nil || !result.Confirmed {
		t.Fatalf("mint result=%+v err=%v", result, err)
	}
	if _, err := adapter.Mint(ctx, request); !errors.Is(err, ErrChainReverted) {
		t.Fatalf("duplicate contract operation must be rejected without another mint, got %v", err)
	}
	supply, _, err := adapter.TotalSupply(ctx, contractAddress)
	if err != nil || supply.String() != "2" {
		t.Fatalf("replayed mint changed supply: supply=%s err=%v", supply.String(), err)
	}
	status, err := adapter.Operation(ctx, contractAddress, mintOperation)
	if err != nil || !status.Executed {
		t.Fatalf("mint operation status=%+v err=%v", status, err)
	}

	burnRequest := OperationRequest{ContractAddress: contractAddress, OperationID: strings.Repeat("2", 64), Account: vault, Quantity: money.MustParse("1")}
	if result, err := adapter.Burn(ctx, burnRequest); err != nil || !result.Confirmed {
		t.Fatalf("burn result=%+v err=%v", result, err)
	}
	forcedRequest := OperationRequest{ContractAddress: contractAddress, OperationID: strings.Repeat("3", 64), Account: vault, Quantity: money.MustParse("1")}
	if result, err := adapter.ForcedRedemption(ctx, forcedRequest); err != nil || !result.Confirmed {
		t.Fatalf("forced redemption result=%+v err=%v", result, err)
	}
	supply, _, err = adapter.TotalSupply(ctx, contractAddress)
	if err != nil || !supply.IsZero() {
		t.Fatalf("burns did not clear supply: supply=%s err=%v", supply.String(), err)
	}
}
