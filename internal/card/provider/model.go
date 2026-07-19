package provider

import (
	"context"
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/money"
)

type Scenario string

const (
	ScenarioNormal           Scenario = "NORMAL"
	ScenarioDecline          Scenario = "DECLINE"
	ScenarioPartial          Scenario = "PARTIAL"
	ScenarioTimeout          Scenario = "TIMEOUT"
	ScenarioStale            Scenario = "STALE"
	ScenarioVenueFailure     Scenario = "VENUE_FAILURE"
	ScenarioProtectionBreach Scenario = "PROTECTION_BREACH"
)

type Capabilities struct {
	Mode                string
	VirtualCards        bool
	PhysicalCards       bool
	MetalCards          bool
	NFCTerminal         bool
	AppleWalletStatus   string
	GoogleWalletStatus  string
	DeterministicReplay bool
}

type ProvisionRequest struct {
	CustomerReference string
	CardType          string
	IdempotencyKey    string
}

type ProvisionedCard struct {
	ProviderCardReference string
	Last4                 string
	InitialStatus         string
	AppleWalletStatus     string
	GoogleWalletStatus    string
}

type FXQuote struct {
	Currency   string
	USDPerUnit money.Decimal
	ObservedAt time.Time
	Status     string
}

type ProtectedSellRequest struct {
	ClientOrderReference string
	Symbol               string
	RequestedQuantity    money.Decimal
	ReferencePrice       money.Decimal
	ProtectedLimitPrice  money.Decimal
	Scenario             Scenario
}

type ProtectedSellResult struct {
	ProviderOrderID string
	FilledQuantity  money.Decimal
	ExecutionPrice  money.Decimal
	ProceedsUSD     money.Decimal
	Status          string
	FailureCode     string
}

type Adapter interface {
	Name() string
	Capabilities(context.Context) Capabilities
	Health(context.Context) error
	Provision(context.Context, ProvisionRequest) (ProvisionedCard, error)
	FX(context.Context, string, Scenario) (FXQuote, error)
	ExecuteProtectedSell(context.Context, ProtectedSellRequest) (ProtectedSellResult, error)
}

var (
	ErrProductionMode      = errors.New("local card simulator is disabled in production")
	ErrInvalidRequest      = errors.New("invalid card provider request")
	ErrUnsupportedCurrency = errors.New("merchant currency is unsupported")
	ErrTimeout             = errors.New("card provider timeout")
	ErrStaleQuote          = errors.New("card provider quote is stale")
	ErrUnavailable         = errors.New("card provider is unavailable")
)
