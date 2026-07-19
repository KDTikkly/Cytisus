package crypto

import (
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/money"
)

type CustomerSession struct {
	PaperAccountID      string
	CustomerReference   string
	CashLedgerAccountID string
}

type Asset struct {
	Symbol       string
	DisplayName  string
	IsStablecoin bool
	Precision    int16
	Networks     []Network
}

type Network struct {
	Code              string
	DisplayName       string
	DepositEnabled    bool
	WithdrawalEnabled bool
	NativeDeployment  bool
}

type PortfolioBalance struct {
	Asset   string
	Settled money.Decimal
	Held    money.Decimal
	Frozen  money.Decimal
}

type Conversion struct {
	ID                 string
	SourceAsset        string
	DestinationAsset   string
	SourceAmount       money.Decimal
	SimulationScenario string
	Status             string
	ReasonCode         string
	NextAction         string
	PolicyVersion      string
	ComplianceCaseID   string
	Legs               []Leg
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Replayed           bool
}

type Leg struct {
	ID                    string
	Sequence              int16
	Side                  string
	Asset                 string
	QuoteCurrency         string
	InputAmount           money.Decimal
	FilledQuantity        money.Decimal
	ReferencePrice        money.Decimal
	AverageExecutionPrice money.Decimal
	GrossUSD              money.Decimal
	VenueFeeUSD           money.Decimal
	PlatformFeeUSD        money.Decimal
	FinalCustomerUSD      money.Decimal
	PriceImprovementUSD   money.Decimal
	Status                string
	FailureCode           string
	LedgerTransactionID   string
	Children              []ChildOrder
}

type ChildOrder struct {
	ID                 string
	Sequence           int16
	Venue              string
	ClientOrderID      string
	ProviderOrderID    string
	RequestedQuantity  money.Decimal
	FilledQuantity     money.Decimal
	QuoteBid           money.Decimal
	QuoteAsk           money.Decimal
	VenueFeeRate       money.Decimal
	EffectiveUnitPrice money.Decimal
	ExecutionPrice     money.Decimal
	GrossUSD           money.Decimal
	VenueFeeUSD        money.Decimal
	Status             string
	FailureCode        string
	QuoteObservedAt    time.Time
	Fills              []Fill
}

type Fill struct {
	ID             string
	Venue          string
	ExternalFillID string
	Quantity       money.Decimal
	Price          money.Decimal
	GrossUSD       money.Decimal
	VenueFeeUSD    money.Decimal
	OccurredAt     time.Time
}

type DepositAddress struct {
	ID              string
	Asset           string
	Network         string
	Provider        string
	ExternalAddress string
	Memo            string
	Status          string
	CreatedAt       time.Time
	Replayed        bool
}

type Deposit struct {
	ID                    string
	DepositAddressID      string
	Asset                 string
	Network               string
	Quantity              money.Decimal
	Status                string
	Provider              string
	ProviderTransactionID string
	LedgerTransactionID   string
	ReasonCode            string
	CreatedAt             time.Time
	Replayed              bool
}

type WithdrawalAddress struct {
	ID              string
	Asset           string
	Network         string
	ExternalAddress string
	Label           string
	Status          string
	RiskLevel       string
	CoolingUntil    *time.Time
	CoolingReason   string
	PolicyVersion   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Replayed        bool
}

type Withdrawal struct {
	ID                   string
	WithdrawalAddressID  string
	Asset                string
	Network              string
	Quantity             money.Decimal
	Status               string
	ReasonCode           string
	NextAction           string
	Provider             string
	ProviderWithdrawalID string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Replayed             bool
}

type ReconciliationRun struct {
	ID             string
	Asset          string
	LedgerAmount   money.Decimal
	CustodyAmount  money.Decimal
	Difference     money.Decimal
	Status         string
	ComplianceCase string
	CompletedAt    time.Time
}

type ConvertCommand struct {
	AccessToken        string
	IdempotencyKey     string
	SourceAsset        string
	DestinationAsset   string
	SourceAmount       money.Decimal
	SimulationScenario provider.Scenario
	SimulationRiskFlag bool
}

type CreateDepositAddressCommand struct {
	AccessToken    string
	IdempotencyKey string
	Asset          string
	Network        string
}

type DepositEvent struct {
	Provider              string
	ExternalEventID       string
	DepositAddressID      string
	Asset                 string
	Network               string
	Quantity              money.Decimal
	EventType             string
	ProviderTransactionID string
	ReasonCode            string
	OccurredAt            time.Time
	Payload               []byte
}

type AddressRiskContext struct {
	UntrustedDevice      bool
	RecentSecurityChange bool
	RecentRecovery       bool
}

type AddWithdrawalAddressCommand struct {
	AccessToken     string
	IdempotencyKey  string
	Asset           string
	Network         string
	ExternalAddress string
	Label           string
	RiskContext     AddressRiskContext
}

type ActivateWithdrawalAddressCommand struct {
	AccessToken string
	AddressID   string
}

type RequestWithdrawalCommand struct {
	AccessToken    string
	IdempotencyKey string
	AddressID      string
	Quantity       money.Decimal
}

type WithdrawalEvent struct {
	Provider        string
	ExternalEventID string
	WithdrawalID    string
	EventType       string
	ReasonCode      string
	OccurredAt      time.Time
	Payload         []byte
}

type AdminActor struct {
	ID   string
	Role string
}

type ReconcileCommand struct {
	Actor             AdminActor
	CustomerReference string
	Asset             string
	CustodyAmount     *money.Decimal
}

var (
	ErrUnauthorized             = errors.New("crypto session is unauthorized")
	ErrInvalidCommand           = errors.New("invalid crypto command")
	ErrAssetNotFound            = errors.New("crypto asset or network not found")
	ErrUnsupportedPair          = errors.New("only explicit crypto/USD legs are supported")
	ErrInsufficientCash         = errors.New("insufficient settled and withdrawable USD")
	ErrInsufficientAsset        = errors.New("insufficient settled crypto quantity")
	ErrIdempotencyConflict      = errors.New("idempotency key was reused with different crypto request data")
	ErrProviderEventConflict    = errors.New("provider event was reused with different crypto callback data")
	ErrConversionNotFound       = errors.New("crypto conversion not found")
	ErrRoutingFailed            = errors.New("no executable simulated crypto liquidity")
	ErrAddressNotFound          = errors.New("crypto address not found")
	ErrCoolingOff               = errors.New("crypto withdrawal address cooling period is active")
	ErrAddressRiskBlocked       = errors.New("crypto address did not pass risk review")
	ErrWithdrawalNotFound       = errors.New("crypto withdrawal not found")
	ErrInvalidState             = errors.New("crypto state does not allow this action")
	ErrReviewRequired           = errors.New("crypto-to-USD conversion requires review")
	ErrAdminUnauthorized        = errors.New("admin role is not authorized for crypto reconciliation")
	ErrReconciliationDifference = errors.New("crypto reconciliation difference requires review")
)
