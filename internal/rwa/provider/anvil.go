package provider

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

const (
	mintSelector             = "7ed9db59"
	burnSelector             = "346a9074"
	forcedRedemptionSelector = "d67db9ca"
	operationSelector        = "3e3eed84"
	totalSupplySelector      = "18160ddd"
	setPermissionSelector    = "ec6263c0"
)

var addressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

type AnvilConfig struct {
	RPCURL       string
	AdminAddress string
	Timeout      time.Duration
	HTTPClient   *http.Client
}

type AnvilAdapter struct {
	rpc     *rpcClient
	admin   string
	timeout time.Duration
	now     func() time.Time
}

func NewAnvilAdapter(environment config.Environment, configuration AnvilConfig) (*AnvilAdapter, error) {
	if environment == config.EnvironmentProduction {
		return nil, ErrProductionMode
	}
	if strings.TrimSpace(configuration.RPCURL) == "" || !validAddress(configuration.AdminAddress) {
		return nil, ErrInvalidRequest
	}
	if configuration.Timeout <= 0 {
		configuration.Timeout = 3 * time.Second
	}
	client := configuration.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: configuration.Timeout}
	}
	return &AnvilAdapter{
		rpc:   &rpcClient{url: configuration.RPCURL, client: client},
		admin: configuration.AdminAddress, timeout: configuration.Timeout, now: time.Now,
	}, nil
}

func (adapter *AnvilAdapter) Name() string { return "local-anvil-rwa-simulator" }

func (adapter *AnvilAdapter) Capabilities(context.Context) (Capability, error) {
	return Capability{
		Provider: adapter.Name(), ChainName: "BASE_ANVIL", ChainID: BaseAnvilChainID,
		Simulated: true, Permissioned: true, WholeSharesOnly: true, BridgeSupported: false,
		ForcedRedemption: true, OperationReplay: true,
		SimulationMessage: "SIMULATION_ONLY_NOT_A_LEGALLY_ISSUED_SECURITY",
	}, nil
}

func (adapter *AnvilAdapter) Health(ctx context.Context) (Health, error) {
	chain, err := adapter.chainID(ctx)
	if err != nil {
		return Health{}, err
	}
	if chain != BaseAnvilChainID {
		return Health{}, fmt.Errorf("%w: got %d", ErrWrongChain, chain)
	}
	var block string
	if err := adapter.rpc.call(ctx, "eth_blockNumber", []any{}, &block); err != nil {
		return Health{}, err
	}
	return Health{Status: "UP", ChainID: chain, BlockNumber: block, CheckedAt: adapter.now().UTC()}, nil
}

func (adapter *AnvilAdapter) Mint(ctx context.Context, request OperationRequest) (OperationResult, error) {
	data, err := encodeOperation(mintSelector, request)
	if err != nil {
		return OperationResult{}, err
	}
	return adapter.submit(ctx, request.OperationID, request.ContractAddress, data)
}

func (adapter *AnvilAdapter) Burn(ctx context.Context, request OperationRequest) (OperationResult, error) {
	data, err := encodeOperation(burnSelector, request)
	if err != nil {
		return OperationResult{}, err
	}
	return adapter.submit(ctx, request.OperationID, request.ContractAddress, data)
}

func (adapter *AnvilAdapter) ForcedRedemption(ctx context.Context, request OperationRequest) (OperationResult, error) {
	data, err := encodeOperation(forcedRedemptionSelector, request)
	if err != nil {
		return OperationResult{}, err
	}
	return adapter.submit(ctx, request.OperationID, request.ContractAddress, data)
}

func (adapter *AnvilAdapter) SetPermission(ctx context.Context, contractAddress, account string, allowed bool) (OperationResult, error) {
	if !validAddress(contractAddress) || !validAddress(account) {
		return OperationResult{}, ErrInvalidRequest
	}
	flag := "0"
	if allowed {
		flag = "1"
	}
	data := "0x" + setPermissionSelector + padAddress(account) + padUint(flag)
	operationID := strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(account), "0x")
	return adapter.submit(ctx, operationID, contractAddress, data)
}

func (adapter *AnvilAdapter) Operation(ctx context.Context, contractAddress, operationID string) (OperationStatus, error) {
	if !validAddress(contractAddress) || !validOperationID(operationID) {
		return OperationStatus{}, ErrInvalidRequest
	}
	data := "0x" + operationSelector + operationID
	result, err := adapter.ethCall(ctx, contractAddress, data)
	if err != nil {
		return OperationStatus{}, err
	}
	var block string
	if err := adapter.rpc.call(ctx, "eth_blockNumber", []any{}, &block); err != nil {
		return OperationStatus{}, err
	}
	return OperationStatus{OperationID: operationID, Executed: parseHexBig(result).Sign() != 0, BlockNumber: block}, nil
}

func (adapter *AnvilAdapter) TotalSupply(ctx context.Context, contractAddress string) (money.Decimal, string, error) {
	if !validAddress(contractAddress) {
		return money.Decimal{}, "", ErrInvalidRequest
	}
	result, err := adapter.ethCall(ctx, contractAddress, "0x"+totalSupplySelector)
	if err != nil {
		return money.Decimal{}, "", err
	}
	supply, err := money.Parse(parseHexBig(result).String())
	if err != nil {
		return money.Decimal{}, "", fmt.Errorf("decode total supply: %w", err)
	}
	var block string
	if err := adapter.rpc.call(ctx, "eth_blockNumber", []any{}, &block); err != nil {
		return money.Decimal{}, "", err
	}
	return supply, block, nil
}

func (adapter *AnvilAdapter) submit(ctx context.Context, operationID, contractAddress, data string) (OperationResult, error) {
	if !validOperationID(operationID) || !validAddress(contractAddress) {
		return OperationResult{}, ErrInvalidRequest
	}
	chain, err := adapter.chainID(ctx)
	if err != nil {
		return OperationResult{}, err
	}
	if chain != BaseAnvilChainID {
		return OperationResult{}, ErrWrongChain
	}
	var transactionHash string
	err = adapter.rpc.call(ctx, "eth_sendTransaction", []any{map[string]string{
		"from": adapter.admin, "to": contractAddress, "data": data, "gas": "0x2dc6c0",
	}}, &transactionHash)
	if err != nil {
		return OperationResult{}, err
	}
	result := OperationResult{OperationID: operationID, ExternalEventID: transactionHash, TransactionHash: transactionHash}
	receiptContext, cancel := context.WithTimeout(ctx, adapter.timeout)
	defer cancel()
	for {
		var receipt *transactionReceipt
		if err := adapter.rpc.call(receiptContext, "eth_getTransactionReceipt", []any{transactionHash}, &receipt); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(receiptContext.Err(), context.DeadlineExceeded) {
				result.OutcomeUnknown = true
				return result, ErrOutcomeUnknown
			}
			return result, err
		}
		if receipt != nil {
			payload, _ := json.Marshal(receipt)
			result.Payload = payload
			if receipt.Status != "0x1" {
				return result, ErrChainReverted
			}
			result.Confirmed = true
			return result, nil
		}
		select {
		case <-receiptContext.Done():
			result.OutcomeUnknown = true
			return result, ErrOutcomeUnknown
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (adapter *AnvilAdapter) ethCall(ctx context.Context, contractAddress, data string) (string, error) {
	var result string
	if err := adapter.rpc.call(ctx, "eth_call", []any{map[string]string{
		"from": adapter.admin, "to": contractAddress, "data": data,
	}, "latest"}, &result); err != nil {
		return "", err
	}
	return result, nil
}

func (adapter *AnvilAdapter) chainID(ctx context.Context) (int64, error) {
	var value string
	if err := adapter.rpc.call(ctx, "eth_chainId", []any{}, &value); err != nil {
		return 0, err
	}
	chain, err := strconv.ParseInt(strings.TrimPrefix(value, "0x"), 16, 64)
	if err != nil {
		return 0, fmt.Errorf("decode chain ID: %w", ErrUnavailable)
	}
	return chain, nil
}

func encodeOperation(selector string, request OperationRequest) (string, error) {
	if !validOperationID(request.OperationID) || !validAddress(request.Account) ||
		!validAddress(request.ContractAddress) || !request.Quantity.IsPositive() || !request.Quantity.IsInteger() {
		return "", ErrInvalidRequest
	}
	quantity, ok := new(big.Int).SetString(request.Quantity.String(), 10)
	if !ok || quantity.Sign() <= 0 {
		return "", ErrInvalidRequest
	}
	return "0x" + selector + request.OperationID + padAddress(request.Account) + fmt.Sprintf("%064x", quantity), nil
}

func padAddress(address string) string {
	return strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(address), "0x")
}

func padUint(value string) string {
	number, _ := new(big.Int).SetString(value, 10)
	return fmt.Sprintf("%064x", number)
}

func validAddress(value string) bool { return addressPattern.MatchString(value) }

func validOperationID(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func parseHexBig(value string) *big.Int {
	result := new(big.Int)
	result.SetString(strings.TrimPrefix(value, "0x"), 16)
	return result
}

type transactionReceipt struct {
	TransactionHash string `json:"transactionHash"`
	BlockNumber     string `json:"blockNumber"`
	Status          string `json:"status"`
}

type rpcClient struct {
	url    string
	client *http.Client
	nextID atomic.Uint64
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (client *rpcClient) call(ctx context.Context, method string, params any, target any) error {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: client.nextID.Add(1), Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encode JSON-RPC request: %w", ErrUnavailable)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create JSON-RPC request: %w", ErrUnavailable)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return fmt.Errorf("call JSON-RPC: %w", ErrUnavailable)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		return fmt.Errorf("read JSON-RPC response: %w", ErrUnavailable)
	}
	var decoded rpcResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return fmt.Errorf("decode JSON-RPC response: %w", ErrUnavailable)
	}
	if decoded.Error != nil {
		message := strings.ToLower(decoded.Error.Message)
		if strings.Contains(message, "revert") {
			return fmt.Errorf("%w: code %d", ErrChainReverted, decoded.Error.Code)
		}
		return fmt.Errorf("%w: code %d", ErrUnavailable, decoded.Error.Code)
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(decoded.Result, target); err != nil {
		return fmt.Errorf("decode JSON-RPC result: %w", ErrUnavailable)
	}
	return nil
}
