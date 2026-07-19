package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

type Local struct {
	now func() time.Time
}

func NewLocal(environment config.Environment) (*Local, error) {
	if environment != config.EnvironmentLocal && environment != config.EnvironmentTest {
		return nil, ErrProductionMode
	}
	return &Local{now: time.Now}, nil
}

func (local *Local) WithClock(now func() time.Time) *Local {
	if now != nil {
		local.now = now
	}
	return local
}

func (*Local) Name() string { return "local-card-simulator" }

func (*Local) Capabilities(context.Context) Capabilities {
	return Capabilities{
		Mode: "SIMULATED", VirtualCards: true, PhysicalCards: true, MetalCards: true,
		NFCTerminal: true, AppleWalletStatus: "UNAVAILABLE_SIMULATOR",
		GoogleWalletStatus: "UNAVAILABLE_SIMULATOR", DeterministicReplay: true,
	}
}

func (*Local) Health(context.Context) error { return nil }

func (local *Local) Provision(_ context.Context, request ProvisionRequest) (ProvisionedCard, error) {
	if strings.TrimSpace(request.CustomerReference) == "" || len(request.IdempotencyKey) < 8 ||
		(request.CardType != "VIRTUAL" && request.CardType != "PLASTIC" && request.CardType != "METAL") {
		return ProvisionedCard{}, ErrInvalidRequest
	}
	digest := sha256.Sum256([]byte(request.CustomerReference + ":" + request.CardType + ":" + request.IdempotencyKey))
	decimal := fmt.Sprintf("%04d", (int(digest[0])<<8|int(digest[1]))%10000)
	status := "CREATED"
	if request.CardType != "VIRTUAL" {
		status = "APPLICATION_SUBMITTED"
	}
	return ProvisionedCard{
		ProviderCardReference: "card_sim_" + hex.EncodeToString(digest[:12]), Last4: decimal,
		InitialStatus: status, AppleWalletStatus: "UNAVAILABLE_SIMULATOR", GoogleWalletStatus: "UNAVAILABLE_SIMULATOR",
	}, nil
}

func (local *Local) FX(_ context.Context, currency string, scenario Scenario) (FXQuote, error) {
	if scenario == ScenarioTimeout {
		return FXQuote{}, ErrTimeout
	}
	if scenario == ScenarioVenueFailure {
		return FXQuote{}, ErrUnavailable
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	rates := map[string]string{
		"USD": "1", "EUR": "1.08", "GBP": "1.27", "JPY": "0.0068", "CAD": "0.74",
	}
	rate, ok := rates[currency]
	if !ok {
		return FXQuote{}, ErrUnsupportedCurrency
	}
	status := "SIMULATED"
	observed := local.now().UTC()
	if scenario == ScenarioStale {
		status = "STALE"
		observed = observed.Add(-24 * time.Hour)
	}
	return FXQuote{Currency: currency, USDPerUnit: money.MustParse(rate), ObservedAt: observed, Status: status}, nil
}

func (*Local) ExecuteProtectedSell(_ context.Context, request ProtectedSellRequest) (ProtectedSellResult, error) {
	if request.ClientOrderReference == "" || request.Symbol == "" || !request.RequestedQuantity.IsPositive() ||
		!request.ReferencePrice.IsPositive() || !request.ProtectedLimitPrice.IsPositive() ||
		request.ProtectedLimitPrice.Compare(request.ReferencePrice) >= 0 {
		return ProtectedSellResult{}, ErrInvalidRequest
	}
	switch request.Scenario {
	case ScenarioTimeout:
		return ProtectedSellResult{}, ErrTimeout
	case ScenarioStale:
		return ProtectedSellResult{}, ErrStaleQuote
	case ScenarioVenueFailure:
		return ProtectedSellResult{}, ErrUnavailable
	case ScenarioProtectionBreach:
		return ProtectedSellResult{
			ProviderOrderID: "protected_" + request.ClientOrderReference, Status: "FAILED",
			FailureCode: "PROTECTED_LIMIT_NOT_MARKETABLE",
		}, nil
	}
	executionPrice, err := request.ReferencePrice.Multiply(money.MustParse("0.995"), money.RoundingPolicy{Version: "card-provider-v1", DecimalPlaces: 18, Mode: money.RoundHalfEven})
	if err != nil {
		return ProtectedSellResult{}, err
	}
	if executionPrice.Compare(request.ProtectedLimitPrice) < 0 {
		return ProtectedSellResult{
			ProviderOrderID: "protected_" + request.ClientOrderReference, Status: "FAILED",
			FailureCode: "PROTECTED_LIMIT_NOT_MARKETABLE",
		}, nil
	}
	filled := request.RequestedQuantity
	status := "FILLED"
	if request.Scenario == ScenarioPartial {
		filled, err = request.RequestedQuantity.Multiply(money.MustParse("0.5"), money.RoundingPolicy{Version: "card-provider-v1", DecimalPlaces: 18, Mode: money.RoundTowardZero})
		if err != nil {
			return ProtectedSellResult{}, err
		}
		status = "PARTIALLY_FILLED"
	}
	proceeds, err := filled.Multiply(executionPrice, money.RoundingPolicy{Version: "card-provider-v1", DecimalPlaces: 18, Mode: money.RoundHalfEven})
	if err != nil {
		return ProtectedSellResult{}, err
	}
	return ProtectedSellResult{
		ProviderOrderID: "protected_" + request.ClientOrderReference, FilledQuantity: filled,
		ExecutionPrice: executionPrice, ProceedsUSD: proceeds, Status: status,
	}, nil
}
