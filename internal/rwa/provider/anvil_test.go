package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

const (
	testAdmin    = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	testVault    = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"
	testContract = "0x5FbDB2315678afecb367f032d93F642f64180aa3"
	testTx       = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testOp       = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestAnvilAdapterMintContract(t *testing.T) {
	t.Parallel()
	var mutex sync.Mutex
	var sentData string
	server := rpcServer(t, func(method string, params json.RawMessage) any {
		switch method {
		case "eth_chainId":
			return "0x14a34"
		case "eth_sendTransaction":
			var values []map[string]string
			if err := json.Unmarshal(params, &values); err != nil {
				t.Fatal(err)
			}
			mutex.Lock()
			sentData = values[0]["data"]
			mutex.Unlock()
			return testTx
		case "eth_getTransactionReceipt":
			return map[string]string{"transactionHash": testTx, "blockNumber": "0x2", "status": "0x1"}
		default:
			t.Fatalf("unexpected method %s", method)
			return nil
		}
	})
	defer server.Close()
	adapter, err := NewAnvilAdapter(config.EnvironmentTest, AnvilConfig{RPCURL: server.URL, AdminAddress: testAdmin, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Mint(context.Background(), OperationRequest{ContractAddress: testContract, OperationID: testOp, Account: testVault, Quantity: money.MustParse("3")})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Confirmed || result.TransactionHash != testTx || result.ExternalEventID != testTx {
		t.Fatalf("unexpected result: %+v", result)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if !strings.HasPrefix(sentData, "0x"+mintSelector+testOp) || len(sentData) != 2+8+64+64+64 {
		t.Fatalf("unexpected mint calldata: %s", sentData)
	}
}

func TestAnvilAdapterOperationAndSupplyContract(t *testing.T) {
	t.Parallel()
	server := rpcServer(t, func(method string, params json.RawMessage) any {
		switch method {
		case "eth_call":
			var values []json.RawMessage
			_ = json.Unmarshal(params, &values)
			var call map[string]string
			_ = json.Unmarshal(values[0], &call)
			if call["data"] == "0x"+totalSupplySelector {
				return "0x7"
			}
			return "0x1"
		case "eth_blockNumber":
			return "0x9"
		default:
			t.Fatalf("unexpected method %s", method)
			return nil
		}
	})
	defer server.Close()
	adapter, _ := NewAnvilAdapter(config.EnvironmentTest, AnvilConfig{RPCURL: server.URL, AdminAddress: testAdmin})
	status, err := adapter.Operation(context.Background(), testContract, testOp)
	if err != nil || !status.Executed || status.BlockNumber != "0x9" {
		t.Fatalf("operation: %+v %v", status, err)
	}
	supply, block, err := adapter.TotalSupply(context.Background(), testContract)
	if err != nil || supply.String() != "7" || block != "0x9" {
		t.Fatalf("supply=%s block=%s err=%v", supply.String(), block, err)
	}
}

func TestAnvilAdapterRejectsFractionalAndWrongChain(t *testing.T) {
	t.Parallel()
	adapter, _ := NewAnvilAdapter(config.EnvironmentTest, AnvilConfig{RPCURL: "http://unused.invalid", AdminAddress: testAdmin})
	_, err := adapter.Mint(context.Background(), OperationRequest{ContractAddress: testContract, OperationID: testOp, Account: testVault, Quantity: money.MustParse("1.5")})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid request, got %v", err)
	}
	server := rpcServer(t, func(method string, _ json.RawMessage) any {
		if method != "eth_chainId" {
			t.Fatalf("unexpected method %s", method)
		}
		return "0x1"
	})
	defer server.Close()
	adapter, _ = NewAnvilAdapter(config.EnvironmentTest, AnvilConfig{RPCURL: server.URL, AdminAddress: testAdmin})
	_, err = adapter.Mint(context.Background(), OperationRequest{ContractAddress: testContract, OperationID: testOp, Account: testVault, Quantity: money.MustParse("1")})
	if !errors.Is(err, ErrWrongChain) {
		t.Fatalf("expected wrong chain, got %v", err)
	}
}

func TestLocalAddressVerifierContractAndProductionGuard(t *testing.T) {
	t.Parallel()
	if _, err := NewLocalAddressVerifier(config.EnvironmentProduction); !errors.Is(err, ErrProductionMode) {
		t.Fatalf("expected production guard, got %v", err)
	}
	verifier, _ := NewLocalAddressVerifier(config.EnvironmentTest)
	proof, err := verifier.VerifyControl(context.Background(), AddressProofRequest{Address: testVault, ProofReference: "sim-proof:fixture"})
	if err != nil || !proof.Verified {
		t.Fatalf("proof=%+v err=%v", proof, err)
	}
	risk, err := verifier.ReviewRisk(context.Background(), "0x000000000000000000000000000000000000dEaD")
	if err != nil || risk.Approved || risk.ReasonCode != "SIMULATED_RISK_REJECTED" {
		t.Fatalf("risk=%+v err=%v", risk, err)
	}
}

func rpcServer(t *testing.T, result func(string, json.RawMessage) any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      uint64          `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": result(body.Method, body.Params)})
	}))
}
