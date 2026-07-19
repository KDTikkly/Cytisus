package provider

import (
	"context"
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/money"
)

const BaseAnvilChainID int64 = 84532

var (
	ErrInvalidRequest = errors.New("invalid RWA provider request")
	ErrUnavailable    = errors.New("RWA provider unavailable")
	ErrChainReverted  = errors.New("RWA chain transaction reverted")
	ErrOutcomeUnknown = errors.New("RWA chain outcome is unknown")
	ErrProductionMode = errors.New("local RWA simulator is disabled in production")
	ErrWrongChain     = errors.New("RWA provider is connected to an unexpected chain")
)

type Capability struct {
	Provider          string `json:"provider"`
	ChainName         string `json:"chain_name"`
	ChainID           int64  `json:"chain_id"`
	Simulated         bool   `json:"simulated"`
	Permissioned      bool   `json:"permissioned"`
	WholeSharesOnly   bool   `json:"whole_shares_only"`
	BridgeSupported   bool   `json:"bridge_supported"`
	ForcedRedemption  bool   `json:"forced_redemption"`
	OperationReplay   bool   `json:"operation_replay"`
	SimulationMessage string `json:"simulation_message"`
}

type Health struct {
	Status      string    `json:"status"`
	ChainID     int64     `json:"chain_id"`
	BlockNumber string    `json:"block_number"`
	CheckedAt   time.Time `json:"checked_at"`
}

type OperationRequest struct {
	ContractAddress string
	OperationID     string
	Account         string
	Quantity        money.Decimal
}

type OperationResult struct {
	OperationID     string
	ExternalEventID string
	TransactionHash string
	Confirmed       bool
	OutcomeUnknown  bool
	Payload         []byte
}

type OperationStatus struct {
	OperationID string
	Executed    bool
	BlockNumber string
}

type AddressProofRequest struct {
	Address        string
	ProofReference string
}

type AddressProofResult struct {
	Verified bool
	Provider string
	Checked  time.Time
}

type AddressRiskResult struct {
	Approved   bool
	ReasonCode string
	Provider   string
	Checked    time.Time
}

type Adapter interface {
	Name() string
	Capabilities(context.Context) (Capability, error)
	Health(context.Context) (Health, error)
	Mint(context.Context, OperationRequest) (OperationResult, error)
	Burn(context.Context, OperationRequest) (OperationResult, error)
	ForcedRedemption(context.Context, OperationRequest) (OperationResult, error)
	Operation(context.Context, string, string) (OperationStatus, error)
	TotalSupply(context.Context, string) (money.Decimal, string, error)
	SetPermission(context.Context, string, string, bool) (OperationResult, error)
}

type AddressVerifier interface {
	VerifyControl(context.Context, AddressProofRequest) (AddressProofResult, error)
	ReviewRisk(context.Context, string) (AddressRiskResult, error)
}
