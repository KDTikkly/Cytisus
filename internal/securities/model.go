package securities

import (
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
)

const PolicyVersion = "paper-securities-v1"

type PaperAccount struct {
	ID                string
	FixtureID         string
	CustomerReference string
	InitialCash       money.Decimal
	CreatedAt         time.Time
}

type Registration struct {
	Account     PaperAccount
	AccessToken string
	Replayed    bool
}

type OrderStatus string

const (
	OrderPendingSubmission OrderStatus = "PENDING_SUBMISSION"
	OrderOpen              OrderStatus = "OPEN"
	OrderPartiallyFilled   OrderStatus = "PARTIALLY_FILLED"
	OrderFilled            OrderStatus = "FILLED"
	OrderCancelled         OrderStatus = "CANCELLED"
	OrderRejected          OrderStatus = "REJECTED"
	OrderExpired           OrderStatus = "EXPIRED"
)

type SubmitOrderCommand struct {
	AccessToken    string
	IdempotencyKey string
	Symbol         string
	Side           broker.Side
	OrderType      broker.OrderType
	TimeInForce    broker.TimeInForce
	Quantity       money.Decimal
	LimitPrice     *money.Decimal
}

type ActionCommand struct {
	AccessToken    string
	IdempotencyKey string
	OrderID        string
}

type Fill struct {
	ID              string
	ExternalEventID string
	Sequence        int32
	Quantity        money.Decimal
	Price           money.Decimal
	Consideration   money.Decimal
	OccurredAt      time.Time
}

type Order struct {
	ID                  string
	Symbol              string
	Side                broker.Side
	OrderType           broker.OrderType
	TimeInForce         broker.TimeInForce
	Quantity            money.Decimal
	LimitPrice          *money.Decimal
	Status              OrderStatus
	RejectionCode       string
	FilledQuantity      money.Decimal
	AverageFillPrice    *money.Decimal
	ReferencePrice      money.Decimal
	QuoteStatus         marketdata.QuoteStatus
	QuoteObservedAt     time.Time
	ReplayCursor        int32
	ProviderOrderID     string
	DeterministicReplay bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Fills               []Fill
}

type Position struct {
	Symbol      string
	Quantity    money.Decimal
	AverageCost money.Decimal
	CostBasis   money.Decimal
	RealizedPnL money.Decimal
	MarketPrice money.Decimal
	MarketValue money.Decimal
	QuoteStatus marketdata.QuoteStatus
	UpdatedAt   time.Time
}

type CashSummary struct {
	Settled                money.Decimal
	Withdrawable           money.Decimal
	ProvisionalBuyingPower money.Decimal
	TotalBuyingPower       money.Decimal
}

type Portfolio struct {
	Cash      CashSummary
	Positions []Position
}

var (
	ErrUnauthorized          = errors.New("paper account session is unauthorized")
	ErrFixtureDisabled       = errors.New("registration fixtures are disabled in this environment")
	ErrInvalidCommand        = errors.New("invalid securities command")
	ErrIdempotencyConflict   = errors.New("idempotency key was reused with different securities request data")
	ErrInstrumentViewOnly    = errors.New("instrument is view-only")
	ErrFractionalUnsupported = errors.New("instrument does not support fractional quantities")
	ErrQuoteStale            = errors.New("quote is stale")
	ErrQuoteUnavailable      = errors.New("quote is unavailable")
	ErrInsufficientCash      = errors.New("insufficient paper buying power")
	ErrInsufficientPosition  = errors.New("insufficient paper position")
	ErrOrderNotFound         = errors.New("order not found")
	ErrInvalidOrderState     = errors.New("order state does not allow this action")
	ErrReplayExhausted       = errors.New("deterministic replay has no next step")
)
