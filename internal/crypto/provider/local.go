package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

var localPrices = map[string]string{
	"BTC": "68420.50", "ETH": "3588.25", "SOL": "168.42", "XRP": "0.6125",
	"BNB": "612.75", "DOGE": "0.1425", "ADA": "0.4521", "AVAX": "38.44",
	"LINK": "18.62", "LTC": "84.35", "USDC": "0.9987", "USDT": "1.0018",
}

type LocalVenue struct {
	code string
	now  func() time.Time
}

func NewLocalVenue(environment config.Environment, code string) (*LocalVenue, error) {
	if environment == config.EnvironmentProduction {
		return nil, ErrProductionMode
	}
	if code != "VENUE_A" && code != "VENUE_B" && code != "VENUE_C" {
		return nil, ErrInvalidRequest
	}
	return &LocalVenue{code: code, now: time.Now}, nil
}

func (venue *LocalVenue) Name() string {
	return strings.ToLower(strings.ReplaceAll(venue.code, "_", "-"))
}

func (venue *LocalVenue) Capabilities(context.Context) (VenueCapabilities, error) {
	return VenueCapabilities{
		USDOnlyPairs: true, SplitOrders: true, PartialFills: true, ExplicitVenueFees: true,
		PriceImprovement: true, StablecoinMarkets: true,
	}, nil
}

func (venue *LocalVenue) Health(context.Context) (Health, error) {
	return Health{Healthy: true, Mode: "SIMULATED", CheckedAt: venue.now().UTC()}, nil
}

func (venue *LocalVenue) Quote(_ context.Context, request QuoteRequest) (Quote, error) {
	baseText, ok := localPrices[request.Asset]
	if !ok || !validScenario(request.Scenario) {
		return Quote{}, ErrInvalidRequest
	}
	if request.Scenario == ScenarioTimeout && venue.code == "VENUE_B" {
		return Quote{}, ErrTimeout
	}
	if request.Scenario == ScenarioVenueFailure && venue.code == "VENUE_C" {
		return Quote{}, ErrUnavailable
	}
	if request.Scenario == ScenarioStablecoinDepeg {
		switch request.Asset {
		case "USDC":
			baseText = "0.9725"
		case "USDT":
			baseText = "0.9430"
		}
	}
	base := money.MustParse(baseText)
	spread, fee, capacity := venueParameters(venue.code, request.Asset, request.Scenario)
	bid, err := base.Sub(spread)
	if err != nil {
		return Quote{}, err
	}
	ask, err := base.Add(spread)
	if err != nil {
		return Quote{}, err
	}
	observedAt := venue.now().UTC()
	status := QuoteExecutable
	if request.Scenario == ScenarioStale && venue.code == "VENUE_A" {
		status = QuoteStale
		observedAt = observedAt.Add(-10 * time.Minute)
	}
	return Quote{
		Venue: venue.code, Asset: request.Asset, Bid: bid, Ask: ask, FeeRate: fee,
		AvailableQuantity: capacity, Status: status, ObservedAt: observedAt, Simulated: true,
	}, nil
}

func (venue *LocalVenue) Execute(_ context.Context, request ExecutionRequest) (Execution, error) {
	if request.ClientOrderID == "" || request.Asset == "" || !request.Quantity.IsPositive() ||
		(request.Side != SideBuy && request.Side != SideSell) || request.Quote.Venue != venue.code {
		return Execution{}, ErrInvalidRequest
	}
	if request.Scenario == ScenarioTimeout && venue.code == "VENUE_B" {
		return Execution{}, ErrTimeout
	}
	if request.Scenario == ScenarioVenueFailure && venue.code == "VENUE_C" {
		return Execution{}, ErrUnavailable
	}
	filled := request.Quantity
	status := "FILLED"
	if request.Scenario == ScenarioPartial && venue.code == "VENUE_B" {
		var err error
		filled, err = request.Quantity.Divide(money.MustParse("2"), quantityPolicy)
		if err != nil {
			return Execution{}, err
		}
		status = "PARTIALLY_FILLED"
	}
	price := request.Quote.Ask
	if request.Side == SideSell {
		price = request.Quote.Bid
	}
	digest := sha256.Sum256([]byte(venue.code + ":" + request.ClientOrderID))
	reference := hex.EncodeToString(digest[:12])
	payload, _ := json.Marshal(map[string]string{
		"asset": request.Asset, "filled_quantity": filled.String(), "mode": "SIMULATED",
		"price": price.String(), "side": string(request.Side), "venue": venue.code,
	})
	return Execution{
		Venue: venue.code, ProviderOrderID: "sim-order-" + reference,
		ExternalFillID: "sim-fill-" + reference, FilledQuantity: filled, Price: price,
		Status: status, OccurredAt: venue.now().UTC(), Payload: payload, Simulated: true,
	}, nil
}

func venueParameters(code, asset string, scenario Scenario) (money.Decimal, money.Decimal, money.Decimal) {
	spread := spreadFor(code, asset)
	fee := money.MustParse("0.001")
	capacity := money.MustParse("100000")
	if asset == "BTC" {
		capacity = money.MustParse("0.08")
	} else if asset == "ETH" {
		capacity = money.MustParse("1.5")
	}
	switch code {
	case "VENUE_A":
		fee = money.MustParse("0.001")
	case "VENUE_B":
		fee = money.MustParse("0.0005")
		if asset == "BTC" {
			capacity = money.MustParse("0.06")
		}
	case "VENUE_C":
		fee = money.MustParse("0.0015")
		if asset == "BTC" {
			capacity = money.MustParse("0.1")
		}
	}
	if scenario == ScenarioSplit {
		capacity = money.MustParse("0.04")
		if asset != "BTC" {
			capacity = money.MustParse("2")
		}
	}
	if scenario == ScenarioPartial {
		capacity = money.MustParse("0.01")
		if asset != "BTC" {
			capacity = money.MustParse("0.25")
		}
	}
	return spread, fee, capacity
}

func spreadFor(code, asset string) money.Decimal {
	byAsset := map[string][3]string{
		"BTC":  {"15", "12", "10"},
		"ETH":  {"0.8", "0.65", "0.55"},
		"SOL":  {"0.08", "0.06", "0.05"},
		"XRP":  {"0.001", "0.0008", "0.0006"},
		"BNB":  {"0.12", "0.1", "0.08"},
		"DOGE": {"0.0005", "0.0004", "0.0003"},
		"ADA":  {"0.0008", "0.0006", "0.0005"},
		"AVAX": {"0.03", "0.025", "0.02"},
		"LINK": {"0.02", "0.015", "0.012"},
		"LTC":  {"0.05", "0.04", "0.03"},
		"USDC": {"0.0005", "0.0004", "0.0003"},
		"USDT": {"0.0006", "0.0005", "0.0004"},
	}
	index := 0
	if code == "VENUE_B" {
		index = 1
	} else if code == "VENUE_C" {
		index = 2
	}
	return money.MustParse(byAsset[asset][index])
}

type LocalCustody struct {
	now func() time.Time
}

func NewLocalCustody(environment config.Environment) (*LocalCustody, error) {
	if environment == config.EnvironmentProduction {
		return nil, ErrProductionMode
	}
	return &LocalCustody{now: time.Now}, nil
}

func (custody *LocalCustody) Name() string { return "local-custody-simulator" }

func (custody *LocalCustody) Capabilities(context.Context) (CustodyCapabilities, error) {
	return CustodyCapabilities{
		Custodial: true, DepositAddresses: true, Withdrawals: true, ApplicationHoldsKeys: false,
	}, nil
}

func (custody *LocalCustody) Health(context.Context) (Health, error) {
	return Health{Healthy: true, Mode: "SIMULATED", CheckedAt: custody.now().UTC()}, nil
}

func (custody *LocalCustody) GenerateDepositAddress(_ context.Context, request DepositAddressRequest) (DepositAddress, error) {
	if request.CustomerReference == "" || request.Asset == "" || request.Network == "" {
		return DepositAddress{}, ErrInvalidRequest
	}
	digest := sha256.Sum256([]byte(request.CustomerReference + ":" + request.Asset + ":" + request.Network))
	reference := hex.EncodeToString(digest[:16])
	return DepositAddress{
		Provider: custody.Name(), ExternalAddress: fmt.Sprintf("sim:%s:%s:%s", strings.ToLower(request.Network), strings.ToLower(request.Asset), reference),
		Simulated: true,
	}, nil
}

func (custody *LocalCustody) PrepareWithdrawal(_ context.Context, request WithdrawalRequest) (WithdrawalResponse, error) {
	if request.ClientReference == "" || request.Asset == "" || request.Network == "" ||
		len(request.ExternalAddress) < 8 || !request.Quantity.IsPositive() {
		return WithdrawalResponse{}, ErrInvalidRequest
	}
	if strings.Contains(strings.ToLower(request.ExternalAddress), "timeout") {
		return WithdrawalResponse{}, ErrTimeout
	}
	digest := sha256.Sum256([]byte("withdrawal:" + request.ClientReference))
	return WithdrawalResponse{
		Provider: custody.Name(), ProviderWithdrawalID: "sim-withdrawal-" + hex.EncodeToString(digest[:12]),
		InitialStatus: "APPROVED", Simulated: true,
	}, nil
}

type LocalChainAnalytics struct {
	now func() time.Time
}

func NewLocalChainAnalytics(environment config.Environment) (*LocalChainAnalytics, error) {
	if environment == config.EnvironmentProduction {
		return nil, ErrProductionMode
	}
	return &LocalChainAnalytics{now: time.Now}, nil
}

func (analytics *LocalChainAnalytics) Name() string { return "local-chain-risk-simulator" }

func (analytics *LocalChainAnalytics) Health(context.Context) (Health, error) {
	return Health{Healthy: true, Mode: "SIMULATED", CheckedAt: analytics.now().UTC()}, nil
}

func (analytics *LocalChainAnalytics) AnalyzeAddress(_ context.Context, request AddressRiskRequest) (AddressRisk, error) {
	if request.Asset == "" || request.Network == "" || len(request.Address) < 8 {
		return AddressRisk{}, ErrInvalidRequest
	}
	lower := strings.ToLower(request.Address)
	if strings.Contains(lower, "timeout") {
		return AddressRisk{}, ErrTimeout
	}
	decision := "ALLOW"
	reason := "CHAIN_RISK_CLEAR"
	if strings.Contains(lower, "blocked") {
		decision = "BLOCK"
		reason = "CHAIN_RISK_BLOCKED"
	} else if strings.Contains(lower, "elevated") {
		decision = "ELEVATED"
		reason = "CHAIN_RISK_ELEVATED"
	}
	return AddressRisk{Provider: analytics.Name(), Decision: decision, ReasonCode: reason, Simulated: true}, nil
}

var quantityPolicy = money.RoundingPolicy{Version: "crypto-provider-quantity-v1", DecimalPlaces: 18, Mode: money.RoundTowardZero}

func validScenario(scenario Scenario) bool {
	switch scenario {
	case ScenarioNormal, ScenarioSplit, ScenarioPartial, ScenarioTimeout, ScenarioStale, ScenarioVenueFailure, ScenarioStablecoinDepeg:
		return true
	default:
		return false
	}
}
