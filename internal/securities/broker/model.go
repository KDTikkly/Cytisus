package broker

import (
	"context"
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
)

type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

type OrderType string

const (
	OrderTypeMarket OrderType = "MARKET"
	OrderTypeLimit  OrderType = "LIMIT"
)

type TimeInForce string

const (
	TimeInForceDay TimeInForce = "DAY"
	TimeInForceGTC TimeInForce = "GTC"
)

type EventType string

const (
	EventOrderOpened   EventType = "ORDER_OPENED"
	EventOrderRejected EventType = "ORDER_REJECTED"
	EventFill          EventType = "FILL"
	EventOrderExpired  EventType = "ORDER_EXPIRED"
)

type Request struct {
	ClientOrderReference string
	Symbol               string
	Side                 Side
	OrderType            OrderType
	TimeInForce          TimeInForce
	Quantity             money.Decimal
	FilledQuantity       money.Decimal
	LimitPrice           *money.Decimal
	ReplayCursor         int32
	Quote                marketdata.Quote
}

type Event struct {
	Provider        string
	ProviderOrderID string
	ExternalEventID string
	Type            EventType
	ReplayCursor    int32
	FillSequence    int32
	FillQuantity    money.Decimal
	FillPrice       money.Decimal
	OccurredAt      time.Time
	RejectionCode   string
	Payload         []byte
}

type Capabilities struct {
	Mode                string
	MarketOrders        bool
	LimitOrders         bool
	DayOrders           bool
	GTCOrders           bool
	Fractional          bool
	PartialFills        bool
	DeterministicReplay bool
}

var (
	ErrSimulatorDisabled = errors.New("local paper broker simulator is disabled in this environment")
	ErrInvalidRequest    = errors.New("invalid broker request")
	ErrProviderTimeout   = errors.New("paper broker provider timed out")
)

type Adapter interface {
	Name() string
	Capabilities(context.Context) Capabilities
	Health(context.Context) error
	Execute(context.Context, Request) (Event, error)
}
