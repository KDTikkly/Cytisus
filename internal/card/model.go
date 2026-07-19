package card

import (
	"errors"
	"time"

	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/money"
)

const PolicyVersion = "card-sim-v1"

type CustomerSession struct {
	PaperAccountID      string
	CustomerReference   string
	CashLedgerAccountID string
}

type RepaymentMode string

const (
	RepaymentCashOnly         RepaymentMode = "CASH_ONLY"
	RepaymentCashThenAutoSell RepaymentMode = "CASH_THEN_AUTO_SELL"
	RepaymentMonthlyStatement RepaymentMode = "MONTHLY_STATEMENT"
)

type CardType string

const (
	TypeVirtual CardType = "VIRTUAL"
	TypePlastic CardType = "PLASTIC"
	TypeMetal   CardType = "METAL"
)

type Card struct {
	ID                    string
	Type                  CardType
	Status                string
	DisplayName           string
	Last4                 string
	PINSet                bool
	Provider              string
	ProviderCardReference string
	AppleWalletStatus     string
	GoogleWalletStatus    string
	ReplacementForCardID  string
	PolicyVersion         string
	Replayed              bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type Profile struct {
	CustomerReference    string
	RepaymentMode        RepaymentMode
	SpendingStatus       string
	SpendingStatusReason string
	PolicyVersion        string
	Cards                []Card
}

type CollateralDriver struct {
	Symbol           string
	DisplayName      string
	AssetClass       string
	Quantity         money.Decimal
	ReferencePrice   money.Decimal
	MarketValueUSD   money.Decimal
	EligibleValueUSD money.Decimal
	HaircutRate      money.Decimal
	QuoteStatus      string
	MarketStatus     string
	Eligible         bool
	ExclusionReason  string
	ObservedAt       time.Time
}

type SpendingPower struct {
	CashEligibleUSD       money.Decimal
	CollateralEligibleUSD money.Decimal
	GrossUSD              money.Decimal
	OutstandingHoldsUSD   money.Decimal
	ReceivableUSD         money.Decimal
	AvailableUSD          money.Decimal
	AbsoluteCapUSD        money.Decimal
	ProviderCapUSD        money.Decimal
	RepaymentMode         RepaymentMode
	SpendingStatus        string
	PolicyVersion         string
	Drivers               []CollateralDriver
	PrimaryExplanation    string
	CalculatedAt          time.Time
}

type AutoSellMandateAsset struct {
	Priority              int16
	Symbol                string
	MinimumRetainQuantity money.Decimal
}

type AutoSellMandate struct {
	ID              string
	Enabled         bool
	AllowFractional bool
	DailyMaxUSD     money.Decimal
	ValidUntil      time.Time
	PolicyVersion   string
	Assets          []AutoSellMandateAsset
	UpdatedAt       time.Time
}

type AutoSellExecution struct {
	ID                  string
	Sequence            int16
	Symbol              string
	RequestedQuantity   money.Decimal
	ProtectedLimitPrice money.Decimal
	ExecutionPrice      money.Decimal
	FilledQuantity      money.Decimal
	ProceedsUSD         money.Decimal
	Status              string
	FailureCode         string
	LedgerTransactionID string
}

type Authorization struct {
	ID                      string
	CardID                  string
	ExternalAuthorizationID string
	MerchantName            string
	MerchantCategoryCode    string
	MerchantAmount          money.Decimal
	MerchantCurrency        string
	AuthorizationFXRate     money.Decimal
	FXMarkupRate            money.Decimal
	AuthorizedUSD           money.Decimal
	Status                  string
	Offline                 bool
	EntryMode               string
	DeclineCode             string
	HoldLedgerTransactionID string
	CapturedUSD             money.Decimal
	ReversedUSD             money.Decimal
	PolicyVersion           string
	Replayed                bool
	OccurredAt              time.Time
	CreatedAt               time.Time
}

type Capture struct {
	ID                            string
	AuthorizationID               string
	ExternalCaptureID             string
	MerchantAmount                money.Decimal
	MerchantCurrency              string
	ClearingFXRate                money.Decimal
	FXMarkupRate                  money.Decimal
	SettledUSD                    money.Decimal
	TipUSD                        money.Decimal
	HoldReleasedUSD               money.Decimal
	CashRepaidUSD                 money.Decimal
	AutoSellRepaidUSD             money.Decimal
	RefundedUSD                   money.Decimal
	Status                        string
	ReceivableLedgerTransactionID string
	RepaymentLedgerTransactionID  string
	AutoSellExecutions            []AutoSellExecution
	Replayed                      bool
	OccurredAt                    time.Time
	CreatedAt                     time.Time
}

type Refund struct {
	ID                     string
	CaptureID              string
	ExternalRefundID       string
	RefundUSD              money.Decimal
	ReceivableReductionUSD money.Decimal
	CashCreditUSD          money.Decimal
	LedgerTransactionID    string
	Replayed               bool
	OccurredAt             time.Time
}

type Dispute struct {
	ID                  string
	CaptureID           string
	CustomerReference   string
	AmountUSD           money.Decimal
	ReasonCode          string
	Status              string
	Outcome             string
	ComplianceCaseID    string
	LedgerTransactionID string
	OpenedAt            time.Time
	ResolvedAt          *time.Time
}

type CreateCardCommand struct {
	AccessToken    string
	IdempotencyKey string
	Type           CardType
}

type CardActionCommand struct {
	AccessToken    string
	IdempotencyKey string
	CardID         string
	Action         string
	ReasonCode     string
}

type ConfigureRepaymentCommand struct {
	AccessToken    string
	IdempotencyKey string
	Mode           RepaymentMode
}

type ConfigureMandateCommand struct {
	AccessToken     string
	IdempotencyKey  string
	Enabled         bool
	AllowFractional bool
	DailyMaxUSD     money.Decimal
	ValidUntil      time.Time
	Assets          []AutoSellMandateAsset
}

type SeedCollateralCommand struct {
	AccessToken    string
	IdempotencyKey string
	Symbol         string
	Quantity       money.Decimal
}

type UpdateCollateralQuoteCommand struct {
	Symbol         string
	ReferencePrice money.Decimal
	QuoteStatus    string
	MarketStatus   string
	ObservedAt     time.Time
}

type AuthorizeCommand struct {
	AccessToken          string
	ExternalEventID      string
	CardID               string
	MerchantName         string
	MerchantCategoryCode string
	MerchantAmount       money.Decimal
	MerchantCurrency     string
	EntryMode            string
	Offline              bool
	OccurredAt           time.Time
	SimulationScenario   cardprovider.Scenario
}

type CaptureCommand struct {
	AccessToken        string
	ExternalEventID    string
	AuthorizationID    string
	MerchantAmount     money.Decimal
	MerchantCurrency   string
	Final              bool
	OccurredAt         time.Time
	SimulationScenario cardprovider.Scenario
}

type ReversalCommand struct {
	AccessToken     string
	ExternalEventID string
	AuthorizationID string
	AmountUSD       money.Decimal
	OccurredAt      time.Time
}

type RefundCommand struct {
	AccessToken     string
	ExternalEventID string
	CaptureID       string
	AmountUSD       money.Decimal
	OccurredAt      time.Time
}

type OpenDisputeCommand struct {
	AccessToken    string
	IdempotencyKey string
	CaptureID      string
	AmountUSD      money.Decimal
	ReasonCode     string
}

type AdminActor struct {
	ID   string
	Role string
}

type ResolveDisputeCommand struct {
	Actor      AdminActor
	DisputeID  string
	Accept     bool
	ReasonCode string
}

type GenerateStatementCommand struct {
	Actor             AdminActor
	CustomerReference string
	PeriodStart       time.Time
	PeriodEnd         time.Time
}

type PayStatementCommand struct {
	AccessToken    string
	IdempotencyKey string
	StatementID    string
}

type AdvancePhysicalCardCommand struct {
	Actor      AdminActor
	CardID     string
	NextStatus string
	ReasonCode string
}

type ReconcileCommand struct {
	Actor                 AdminActor
	CustomerReference     string
	ProviderHoldUSD       *money.Decimal
	ProviderReceivableUSD *money.Decimal
}

type ReconciliationRun struct {
	ID                    string
	CustomerReference     string
	LedgerHoldUSD         money.Decimal
	ProviderHoldUSD       money.Decimal
	LedgerReceivableUSD   money.Decimal
	ProviderReceivableUSD money.Decimal
	DifferenceUSD         money.Decimal
	Status                string
	ComplianceCaseID      string
	CompletedAt           time.Time
}

type Statement struct {
	ID                           string
	CustomerReference            string
	PeriodStart                  string
	PeriodEnd                    string
	AmountDueUSD                 money.Decimal
	Status                       string
	DueAt                        time.Time
	PaidAt                       *time.Time
	RepaymentLedgerTransactionID string
	CreatedAt                    time.Time
}

var (
	ErrUnauthorized             = errors.New("card session is unauthorized")
	ErrAdminUnauthorized        = errors.New("admin is unauthorized for card action")
	ErrSimulatorDisabled        = errors.New("card simulator is disabled in this environment")
	ErrInvalidCommand           = errors.New("invalid card command")
	ErrIdempotencyConflict      = errors.New("card idempotency key was reused with different data")
	ErrProviderEventConflict    = errors.New("card provider event was reused with different data")
	ErrCardNotFound             = errors.New("card not found")
	ErrAuthorizationNotFound    = errors.New("card authorization not found")
	ErrCaptureNotFound          = errors.New("card capture not found")
	ErrDisputeNotFound          = errors.New("card dispute not found")
	ErrInvalidState             = errors.New("card state does not allow this action")
	ErrCardFrozen               = errors.New("card spending is frozen")
	ErrInsufficientPower        = errors.New("insufficient card spending power")
	ErrFXUnavailable            = errors.New("card FX quote is unavailable")
	ErrTipExceeded              = errors.New("card tip exceeds the configured tolerance")
	ErrCaptureExceeded          = errors.New("capture exceeds the remaining authorization")
	ErrRefundExceeded           = errors.New("refund exceeds the remaining captured amount")
	ErrMandateRequired          = errors.New("an active auto-sell mandate is required")
	ErrProtectedSellFailed      = errors.New("protected auto-sell could not cover the card repayment")
	ErrReconciliationDifference = errors.New("card reconciliation has a difference")
)
