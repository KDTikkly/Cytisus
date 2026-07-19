package provider

import (
	"context"
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/money"
)

type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

type Scenario string

const (
	ScenarioNormal          Scenario = "NORMAL"
	ScenarioSplit           Scenario = "SPLIT"
	ScenarioPartial         Scenario = "PARTIAL"
	ScenarioTimeout         Scenario = "TIMEOUT"
	ScenarioStale           Scenario = "STALE"
	ScenarioVenueFailure    Scenario = "VENUE_FAILURE"
	ScenarioStablecoinDepeg Scenario = "STABLECOIN_DEPEG"
)

type QuoteStatus string

const (
	QuoteExecutable QuoteStatus = "EXECUTABLE"
	QuoteStale      QuoteStatus = "STALE"
	QuoteFailed     QuoteStatus = "FAILED"
)

type Health struct {
	Healthy   bool
	Mode      string
	CheckedAt time.Time
}

type VenueCapabilities struct {
	USDOnlyPairs      bool
	SplitOrders       bool
	PartialFills      bool
	ExplicitVenueFees bool
	PriceImprovement  bool
	StablecoinMarkets bool
}

type QuoteRequest struct {
	Asset    string
	Scenario Scenario
}

type Quote struct {
	Venue             string
	Asset             string
	Bid               money.Decimal
	Ask               money.Decimal
	FeeRate           money.Decimal
	AvailableQuantity money.Decimal
	Status            QuoteStatus
	ObservedAt        time.Time
	Simulated         bool
}

type ExecutionRequest struct {
	ClientOrderID string
	Asset         string
	Side          Side
	Quantity      money.Decimal
	Quote         Quote
	Scenario      Scenario
}

type Execution struct {
	Venue           string
	ProviderOrderID string
	ExternalFillID  string
	FilledQuantity  money.Decimal
	Price           money.Decimal
	Status          string
	FailureCode     string
	OccurredAt      time.Time
	Payload         []byte
	Simulated       bool
}

type VenueAdapter interface {
	Name() string
	Capabilities(context.Context) (VenueCapabilities, error)
	Health(context.Context) (Health, error)
	Quote(context.Context, QuoteRequest) (Quote, error)
	Execute(context.Context, ExecutionRequest) (Execution, error)
}

type CustodyCapabilities struct {
	Custodial            bool
	DepositAddresses     bool
	Withdrawals          bool
	ApplicationHoldsKeys bool
}

type DepositAddressRequest struct {
	CustomerReference string
	Asset             string
	Network           string
}

type DepositAddress struct {
	Provider        string
	ExternalAddress string
	Memo            string
	Simulated       bool
}

type WithdrawalRequest struct {
	ClientReference string
	Asset           string
	Network         string
	ExternalAddress string
	Quantity        money.Decimal
}

type WithdrawalResponse struct {
	Provider             string
	ProviderWithdrawalID string
	InitialStatus        string
	Simulated            bool
}

type CustodyAdapter interface {
	Name() string
	Capabilities(context.Context) (CustodyCapabilities, error)
	Health(context.Context) (Health, error)
	GenerateDepositAddress(context.Context, DepositAddressRequest) (DepositAddress, error)
	PrepareWithdrawal(context.Context, WithdrawalRequest) (WithdrawalResponse, error)
}

type AddressRiskRequest struct {
	Asset   string
	Network string
	Address string
}

type AddressRisk struct {
	Provider   string
	Decision   string
	ReasonCode string
	Simulated  bool
}

type ChainAnalyticsAdapter interface {
	Name() string
	Health(context.Context) (Health, error)
	AnalyzeAddress(context.Context, AddressRiskRequest) (AddressRisk, error)
}

var (
	ErrInvalidRequest = errors.New("invalid crypto provider request")
	ErrUnavailable    = errors.New("crypto provider unavailable")
	ErrTimeout        = errors.New("crypto provider timeout")
	ErrProductionMode = errors.New("local crypto simulators are disabled in production")
)
