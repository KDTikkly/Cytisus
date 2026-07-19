package provider

import (
	"errors"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

func TestLocalCardProviderContract(t *testing.T) {
	t.Parallel()
	clock := func() time.Time { return time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC) }
	adapter, err := NewLocal(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	adapter.WithClock(clock)
	capability := adapter.Capabilities(t.Context())
	if capability.Mode != "SIMULATED" || capability.AppleWalletStatus != "UNAVAILABLE_SIMULATOR" ||
		capability.GoogleWalletStatus != "UNAVAILABLE_SIMULATOR" || !capability.NFCTerminal {
		t.Fatalf("unexpected capability: %+v", capability)
	}
	card, err := adapter.Provision(t.Context(), ProvisionRequest{
		CustomerReference: "fixture:card-provider", CardType: "VIRTUAL", IdempotencyKey: "provider-card-0001",
	})
	if err != nil || card.InitialStatus != "CREATED" || len(card.Last4) != 4 {
		t.Fatalf("unexpected provision response: %+v %v", card, err)
	}
	fx, err := adapter.FX(t.Context(), "EUR", ScenarioNormal)
	if err != nil || fx.USDPerUnit.String() != "1.08" || fx.Status != "SIMULATED" {
		t.Fatalf("unexpected FX response: %+v %v", fx, err)
	}
}

func TestProtectedSellNeverFallsBackToMarket(t *testing.T) {
	t.Parallel()
	adapter, _ := NewLocal(config.EnvironmentTest)
	request := ProtectedSellRequest{
		ClientOrderReference: "autosell-0001", Symbol: "VTI", RequestedQuantity: money.MustParse("2"),
		ReferencePrice: money.MustParse("300"), ProtectedLimitPrice: money.MustParse("294"),
	}
	partial, err := adapter.ExecuteProtectedSell(t.Context(), withScenario(request, ScenarioPartial))
	if err != nil || partial.Status != "PARTIALLY_FILLED" || partial.ExecutionPrice.Compare(request.ProtectedLimitPrice) < 0 {
		t.Fatalf("unexpected protected partial fill: %+v %v", partial, err)
	}
	failed, err := adapter.ExecuteProtectedSell(t.Context(), withScenario(request, ScenarioProtectionBreach))
	if err != nil || failed.Status != "FAILED" || failed.FilledQuantity.IsPositive() || failed.FailureCode == "" {
		t.Fatalf("protection breach must not execute: %+v %v", failed, err)
	}
}

func TestLocalCardProviderFailureModes(t *testing.T) {
	t.Parallel()
	if _, err := NewLocal(config.EnvironmentProduction); !errors.Is(err, ErrProductionMode) {
		t.Fatalf("expected production rejection, got %v", err)
	}
	adapter, _ := NewLocal(config.EnvironmentTest)
	if _, err := adapter.FX(t.Context(), "EUR", ScenarioTimeout); !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected timeout, got %v", err)
	}
	if _, err := adapter.FX(t.Context(), "CHF", ScenarioNormal); !errors.Is(err, ErrUnsupportedCurrency) {
		t.Fatalf("expected unsupported currency, got %v", err)
	}
}

func withScenario(request ProtectedSellRequest, scenario Scenario) ProtectedSellRequest {
	request.Scenario = scenario
	return request
}
