package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

func TestLocalVenueContract(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"VENUE_A", "VENUE_B", "VENUE_C"} {
		venue, err := NewLocalVenue(config.EnvironmentTest, code)
		if err != nil {
			t.Fatalf("create %s: %v", code, err)
		}
		capabilities, err := venue.Capabilities(t.Context())
		if err != nil || !capabilities.USDOnlyPairs || !capabilities.ExplicitVenueFees || !capabilities.PriceImprovement {
			t.Fatalf("unexpected %s capabilities: %+v, %v", code, capabilities, err)
		}
		quote, err := venue.Quote(t.Context(), QuoteRequest{Asset: "BTC", Scenario: ScenarioNormal})
		if err != nil || !quote.Simulated || quote.Bid.Compare(quote.Ask) >= 0 || !quote.FeeRate.IsPositive() {
			t.Fatalf("unexpected %s quote: %+v, %v", code, quote, err)
		}
		execution, err := venue.Execute(t.Context(), ExecutionRequest{
			ClientOrderID: "contract-order-0001", Asset: "BTC", Side: SideBuy,
			Quantity: money.MustParse("0.01"), Quote: quote, Scenario: ScenarioNormal,
		})
		if err != nil || !execution.Simulated || !execution.FilledQuantity.Equal(money.MustParse("0.01")) || len(execution.Payload) == 0 {
			t.Fatalf("unexpected %s execution: %+v, %v", code, execution, err)
		}
	}
}

func TestVenueFailureTimeoutStaleAndStablecoinDepeg(t *testing.T) {
	t.Parallel()

	venueA, _ := NewLocalVenue(config.EnvironmentTest, "VENUE_A")
	stale, err := venueA.Quote(t.Context(), QuoteRequest{Asset: "ETH", Scenario: ScenarioStale})
	if err != nil || stale.Status != QuoteStale {
		t.Fatalf("expected stale quote, got %+v, %v", stale, err)
	}
	depeg, err := venueA.Quote(t.Context(), QuoteRequest{Asset: "USDC", Scenario: ScenarioStablecoinDepeg})
	if err != nil || depeg.Ask.Compare(money.MustParse("0.99")) >= 0 {
		t.Fatalf("expected non-par USDC depeg quote, got %+v, %v", depeg, err)
	}
	venueB, _ := NewLocalVenue(config.EnvironmentTest, "VENUE_B")
	if _, err := venueB.Quote(t.Context(), QuoteRequest{Asset: "BTC", Scenario: ScenarioTimeout}); !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected timeout, got %v", err)
	}
	venueC, _ := NewLocalVenue(config.EnvironmentTest, "VENUE_C")
	if _, err := venueC.Quote(t.Context(), QuoteRequest{Asset: "BTC", Scenario: ScenarioVenueFailure}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected venue failure, got %v", err)
	}
}

func TestCustodyAndChainAnalyticsContracts(t *testing.T) {
	t.Parallel()

	custody, err := NewLocalCustody(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := custody.Capabilities(t.Context())
	if err != nil || !capabilities.Custodial || capabilities.ApplicationHoldsKeys {
		t.Fatalf("unexpected custody capabilities: %+v, %v", capabilities, err)
	}
	address, err := custody.GenerateDepositAddress(t.Context(), DepositAddressRequest{
		CustomerReference: "fixture.customer", Asset: "USDC", Network: "BASE",
	})
	if err != nil || !address.Simulated || address.ExternalAddress == "" {
		t.Fatalf("unexpected deposit address: %+v, %v", address, err)
	}
	analytics, err := NewLocalChainAnalytics(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	risk, err := analytics.AnalyzeAddress(context.Background(), AddressRiskRequest{
		Asset: "ETH", Network: "ETHEREUM", Address: "fixture-elevated-address",
	})
	if err != nil || risk.Decision != "ELEVATED" || !risk.Simulated {
		t.Fatalf("unexpected risk result: %+v, %v", risk, err)
	}
}

func TestProductionRejectsAllLocalCryptoProviders(t *testing.T) {
	t.Parallel()

	if _, err := NewLocalVenue(config.EnvironmentProduction, "VENUE_A"); !errors.Is(err, ErrProductionMode) {
		t.Fatalf("expected venue production guard, got %v", err)
	}
	if _, err := NewLocalCustody(config.EnvironmentProduction); !errors.Is(err, ErrProductionMode) {
		t.Fatalf("expected custody production guard, got %v", err)
	}
	if _, err := NewLocalChainAnalytics(config.EnvironmentProduction); !errors.Is(err, ErrProductionMode) {
		t.Fatalf("expected chain analytics production guard, got %v", err)
	}
}
