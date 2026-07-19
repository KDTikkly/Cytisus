package rwa

import (
	"context"
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/money"
)

const (
	PolicyVersion    = "rwa-sim-v1"
	SimulationOnly   = "SIMULATION_ONLY_NOT_A_LEGALLY_ISSUED_SECURITY"
	CustodyVault     = "PLATFORM_VAULT"
	CustodyExternal  = "EXTERNAL_PERMISSIONED"
	AdminRoleOps     = "OPERATIONS"
	AdminRoleRisk    = "RISK_ANALYST"
	AdminRoleAdmin   = "ADMIN"
	AdminRoleAuditor = "AUDITOR"
)

var (
	ErrUnauthorized        = errors.New("RWA session is unauthorized")
	ErrAdminUnauthorized   = errors.New("RWA administrator is unauthorized")
	ErrInvalidCommand      = errors.New("invalid RWA command")
	ErrInvalidState        = errors.New("RWA state does not allow this action")
	ErrIdempotencyConflict = errors.New("idempotency key was reused with different RWA request data")
	ErrAssetNotFound       = errors.New("RWA asset not found")
	ErrAddressNotActive    = errors.New("external RWA address is not active")
	ErrInsufficientShares  = errors.New("insufficient whole settled shares")
	ErrHoldingNotFound     = errors.New("RWA holding not found")
	ErrOperationUnknown    = errors.New("RWA chain operation requires recovery")
	ErrRecoveryRequired    = errors.New("RWA recovery is required")
)

type CustomerSession struct {
	PaperAccountID      string
	CustomerReference   string
	CashLedgerAccountID string
}

type SessionResolver interface {
	ResolveSession(context.Context, string) (CustomerSession, error)
}

type AdminActor struct {
	ID   string
	Role string
}

type Asset struct {
	ID                   string
	InstrumentID         string
	UnderlyingSymbol     string
	TokenName            string
	TokenSymbol          string
	TokenDecimals        int16
	ContractAddress      string
	PlatformVaultAddress string
	ChainName            string
	ChainID              int64
	SimulationDisclaimer string
}

type ExternalAddress struct {
	ID             string
	Address        string
	Status         string
	ProofReference string
	RiskReasonCode string
	CoolingEndsAt  time.Time
	ActivatedAt    time.Time
	SuspendedAt    time.Time
	UpdatedAt      time.Time
}

type Holding struct {
	AssetID            string
	CustodyMode        string
	DestinationAddress string
	Quantity           money.Decimal
	UpdatedAt          time.Time
}

type Mint struct {
	ID                   string
	AssetID              string
	UnderlyingLockID     string
	Quantity             money.Decimal
	CustodyMode          string
	DestinationAddress   string
	OperationID          string
	Status               string
	ChainTransactionHash string
	FailureCode          string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Redemption struct {
	ID                   string
	AssetID              string
	UnderlyingLockID     string
	Quantity             money.Decimal
	SourceAddress        string
	OperationID          string
	Forced               bool
	Status               string
	ChainTransactionHash string
	FailureCode          string
	CompletedAt          time.Time
}

type Reconciliation struct {
	ID               string
	AssetID          string
	ChainSupply      money.Decimal
	LockedShares     money.Decimal
	Difference       money.Decimal
	Status           string
	ComplianceCaseID string
	ObservedBlock    string
	CompletedAt      time.Time
}

type Dividend struct {
	ID                string
	AssetID           string
	ExternalReference string
	USDPerShare       money.Decimal
	RecordAt          time.Time
	PayableAt         time.Time
	Status            string
	UpdatedAt         time.Time
}

type DividendEntitlement struct {
	ID                string
	CustomerReference string
	WholeShares       money.Decimal
	GrossUSD          money.Decimal
	WithholdingUSD    money.Decimal
	NetUSD            money.Decimal
	Status            string
}

type RegisterAddressCommand struct {
	AccessToken    string
	IdempotencyKey string
	Address        string
	ProofReference string
}

type ReviewAddressCommand struct {
	AddressID  string
	Approve    bool
	ReasonCode string
	Actor      AdminActor
}

type MintCommand struct {
	AccessToken       string
	IdempotencyKey    string
	AssetID           string
	Quantity          money.Decimal
	CustodyMode       string
	ExternalAddressID string
}

type RedeemCommand struct {
	AccessToken    string
	IdempotencyKey string
	MintID         string
}

type ForcedRedeemCommand struct {
	CustomerReference string
	IdempotencyKey    string
	MintID            string
	ReasonCode        string
	Actor             AdminActor
}

type ReconcileCommand struct {
	AssetID string
	Actor   AdminActor
}

type AnnounceDividendCommand struct {
	AssetID           string
	ExternalReference string
	USDPerShare       money.Decimal
	RecordAt          time.Time
	PayableAt         time.Time
	Actor             AdminActor
}

type ProcessDividendCommand struct {
	DividendID string
	Actor      AdminActor
}

type RecoverCommand struct {
	OperationID string
	Actor       AdminActor
}
