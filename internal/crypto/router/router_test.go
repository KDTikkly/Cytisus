package router

import (
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

func TestRoutesByExecutableNetPriceAndPassesImprovement(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	result, err := router.Buy(t.Context(), "BTC", money.MustParse("5000"), provider.ScenarioNormal, "route-buy-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Children) == 0 || result.Children[0].Venue != "VENUE_B" {
		t.Fatalf("expected lowest executable net price at VENUE_B, got %+v", result.Children)
	}
	if !result.VenueFeeUSD.IsPositive() || !result.PlatformFeeUSD.IsPositive() || !result.PriceImprovementUSD.IsPositive() {
		t.Fatalf("expected explicit fees and passed-through improvement, got %+v", result)
	}
	if result.FinalCustomerUSD.Compare(money.MustParse("5000")) > 0 {
		t.Fatalf("route exceeded budget: %s", result.FinalCustomerUSD.String())
	}
}

func TestSplitAndPartialRouting(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	split, err := router.Sell(t.Context(), "BTC", money.MustParse("0.1"), provider.ScenarioSplit, "route-split-0001")
	if err != nil {
		t.Fatal(err)
	}
	if split.Status != "FILLED" || len(split.Children) < 3 || !split.FilledQuantity.Equal(money.MustParse("0.1")) {
		t.Fatalf("expected three-way complete split, got %+v", split)
	}
	partial, err := router.Sell(t.Context(), "BTC", money.MustParse("0.1"), provider.ScenarioPartial, "route-partial-0001")
	if err != nil {
		t.Fatal(err)
	}
	if partial.Status != "PARTIALLY_FILLED" || partial.FilledQuantity.Compare(money.MustParse("0.1")) >= 0 {
		t.Fatalf("expected partial fill, got %+v", partial)
	}
}

func TestExcludesTimeoutStaleAndFailedVenues(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	for _, scenario := range []provider.Scenario{provider.ScenarioTimeout, provider.ScenarioStale, provider.ScenarioVenueFailure} {
		result, err := router.Sell(t.Context(), "ETH", money.MustParse("0.25"), scenario, "route-resilience-"+string(scenario))
		if err != nil {
			t.Fatalf("%s: %v", scenario, err)
		}
		if !result.FilledQuantity.IsPositive() {
			t.Fatalf("expected healthy venue fallback for %s, got %+v", scenario, result)
		}
		foundFailure := false
		for _, attempt := range result.Attempts {
			if attempt.FailureCode != "" {
				foundFailure = true
			}
		}
		if !foundFailure {
			t.Fatalf("expected audited failed attempt for %s", scenario)
		}
	}
}

func TestStablecoinIsNotFixedAtOneDollar(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	result, err := router.Sell(t.Context(), "USDT", money.MustParse("100"), provider.ScenarioStablecoinDepeg, "route-depeg-0001")
	if err != nil {
		t.Fatal(err)
	}
	if result.AverageExecutionPrice.Compare(money.MustParse("0.99")) >= 0 {
		t.Fatalf("expected depegged executable price, got %s", result.AverageExecutionPrice.String())
	}
}

func newTestRouter(t *testing.T) *Router {
	t.Helper()
	venues := make([]provider.VenueAdapter, 0, 3)
	for _, code := range []string{"VENUE_A", "VENUE_B", "VENUE_C"} {
		venue, err := provider.NewLocalVenue(config.EnvironmentTest, code)
		if err != nil {
			t.Fatal(err)
		}
		venues = append(venues, venue)
	}
	router, err := New(Dependencies{
		Venues: venues, PlatformFeeRate: money.MustParse("0.002"), QuoteMaxAge: 5 * time.Second,
		ProviderTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return router
}
