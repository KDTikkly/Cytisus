package router

import (
	"time"

	"github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/money"
)

type Attempt struct {
	Venue       string
	Quote       *provider.Quote
	FailureCode string
}

type Child struct {
	Sequence           int16
	Venue              string
	ClientOrderID      string
	ProviderOrderID    string
	ExternalFillID     string
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
	OccurredAt         time.Time
	ProviderPayload    []byte
}

type Result struct {
	Side                  provider.Side
	Asset                 string
	InputAmount           money.Decimal
	FilledQuantity        money.Decimal
	ReferencePrice        money.Decimal
	AverageExecutionPrice money.Decimal
	GrossUSD              money.Decimal
	VenueFeeUSD           money.Decimal
	PlatformFeeUSD        money.Decimal
	FinalCustomerUSD      money.Decimal
	PriceImprovementUSD   money.Decimal
	UnspentUSD            money.Decimal
	Status                string
	FailureCode           string
	Attempts              []Attempt
	Children              []Child
}
