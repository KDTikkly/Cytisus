package marketdata

import (
	"context"
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/money"
)

type AssetType string

const (
	AssetTypeCommonStock   AssetType = "COMMON_STOCK"
	AssetTypeETF           AssetType = "ETF"
	AssetTypeADR           AssetType = "ADR"
	AssetTypeREIT          AssetType = "REIT"
	AssetTypePreferred     AssetType = "PREFERRED"
	AssetTypeClosedEndFund AssetType = "CLOSED_END_FUND"
	AssetTypeETN           AssetType = "ETN"
	AssetTypeWarrant       AssetType = "WARRANT"
	AssetTypeUnit          AssetType = "UNIT"
	AssetTypeRight         AssetType = "RIGHT"
	AssetTypeOther         AssetType = "OTHER"
)

type QuoteStatus string

const (
	QuoteStatusRealTime    QuoteStatus = "REAL_TIME"
	QuoteStatusDelayed     QuoteStatus = "DELAYED"
	QuoteStatusIndicative  QuoteStatus = "INDICATIVE"
	QuoteStatusSimulated   QuoteStatus = "SIMULATED"
	QuoteStatusStale       QuoteStatus = "STALE"
	QuoteStatusUnavailable QuoteStatus = "UNAVAILABLE"
)

type MarketStatus string

const (
	MarketPreMarket  MarketStatus = "PRE_MARKET"
	MarketOpen       MarketStatus = "OPEN"
	MarketAfterHours MarketStatus = "AFTER_HOURS"
	MarketClosed     MarketStatus = "CLOSED"
	MarketHalted     MarketStatus = "HALTED"
)

type Capability struct {
	Searchable         bool
	QuoteEnabled       bool
	PaperTradable      bool
	LiveTradable       bool
	FractionalEnabled  bool
	TransferOutEnabled bool
	RWAMintEnabled     bool
	UserEligible       bool
	DisabledReason     string
	PolicyVersion      string
}

type Instrument struct {
	ID              string
	Symbol          string
	DisplayName     string
	AssetType       AssetType
	PrimaryExchange string
	Currency        money.Currency
	Listed          bool
	Capability      Capability
}

type Quote struct {
	InstrumentID string
	Symbol       string
	ReplayCursor int32
	Bid          money.Decimal
	Ask          money.Decimal
	Last         money.Decimal
	Status       QuoteStatus
	MarketStatus MarketStatus
	ObservedAt   time.Time
	Provider     string
}

type ProviderCapabilities struct {
	Mode                QuoteStatus
	DeterministicReplay bool
	MaximumReplayCursor int32
}

var (
	ErrInstrumentNotFound = errors.New("instrument not found")
	ErrQuoteUnavailable   = errors.New("quote unavailable")
	ErrSimulatorDisabled  = errors.New("local market simulator is disabled in this environment")
)

type Provider interface {
	Name() string
	Capabilities(context.Context) ProviderCapabilities
	Health(context.Context) error
	Quote(context.Context, string, int32) (Quote, error)
}
