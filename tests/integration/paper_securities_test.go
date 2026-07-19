//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	marketprovider "github.com/KDTikkly/Cytisus/internal/marketdata/provider"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
	brokerprovider "github.com/KDTikkly/Cytisus/internal/securities/provider"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPaperSecuritiesVerticalSlice(t *testing.T) {
	pool := newFinancialTestPool(t)
	service := newPaperService(t, pool, nil, 0)

	registration, err := service.RegisterFixture(t.Context(), "paper.vertical-slice")
	if err != nil {
		t.Fatal(err)
	}
	replayedRegistration, err := service.RegisterFixture(t.Context(), "paper.vertical-slice")
	if err != nil || !replayedRegistration.Replayed || replayedRegistration.AccessToken != registration.AccessToken {
		t.Fatalf("unexpected registration replay: %+v, %v", replayedRegistration, err)
	}

	instruments, err := service.SearchInstruments(t.Context(), "fix", 20)
	if err != nil {
		t.Fatal(err)
	}
	viewOnly := 0
	for _, instrument := range instruments {
		if !instrument.Capability.PaperTradable {
			viewOnly++
			if instrument.Capability.DisabledReason != "ASSET_TYPE_VIEW_ONLY" {
				t.Fatalf("expected %s to expose its disabled reason: %+v", instrument.Symbol, instrument.Capability)
			}
		}
	}
	if viewOnly != 8 {
		t.Fatalf("expected all eight U.S. listed view-only types, got %d", viewOnly)
	}
	staleQuote, err := service.Quote(t.Context(), "FIXREIT", 0)
	if err != nil || staleQuote.Status != marketdata.QuoteStatusStale {
		t.Fatalf("expected an honestly labeled stale quote, got %+v, %v", staleQuote, err)
	}

	command := securities.SubmitOrderCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-order-buy-0001",
		Symbol:         "AAPL",
		Side:           broker.SideBuy,
		OrderType:      broker.OrderTypeMarket,
		TimeInForce:    broker.TimeInForceDay,
		Quantity:       money.MustParse("1.5"),
	}
	partial, err := service.SubmitOrder(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Status != securities.OrderPartiallyFilled || partial.FilledQuantity.String() != "0.75" || partial.QuoteStatus != marketdata.QuoteStatusSimulated {
		t.Fatalf("unexpected initial partial fill: %+v", partial)
	}
	replayedOrder, err := service.SubmitOrder(t.Context(), command)
	if err != nil || replayedOrder.ID != partial.ID || replayedOrder.FilledQuantity.String() != "0.75" {
		t.Fatalf("unexpected order replay: %+v, %v", replayedOrder, err)
	}

	filled, err := service.AdvanceReplay(t.Context(), securities.ActionCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-order-replay-0001",
		OrderID:        partial.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filled.Status != securities.OrderFilled || filled.FilledQuantity.String() != "1.5" || len(filled.Fills) != 2 {
		t.Fatalf("unexpected completed order: %+v", filled)
	}
	if _, err := service.CancelOrder(t.Context(), securities.ActionCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-order-cancel-0001",
		OrderID:        filled.ID,
	}); !errors.Is(err, securities.ErrInvalidOrderState) {
		t.Fatalf("expected invalid terminal transition, got %v", err)
	}

	beforeSell, err := service.Portfolio(t.Context(), registration.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if beforeSell.Cash.Settled.String() != "99714.88" || beforeSell.Cash.Withdrawable.String() != "99714.88" ||
		!beforeSell.Cash.ProvisionalBuyingPower.IsZero() || len(beforeSell.Positions) != 1 || beforeSell.Positions[0].Quantity.String() != "1.5" {
		t.Fatalf("unexpected portfolio after buy: %+v", beforeSell)
	}

	sell, err := service.SubmitOrder(t.Context(), securities.SubmitOrderCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-order-sell-0001",
		Symbol:         "AAPL",
		Side:           broker.SideSell,
		OrderType:      broker.OrderTypeMarket,
		TimeInForce:    broker.TimeInForceGTC,
		Quantity:       money.MustParse("0.5"),
	})
	if err != nil || sell.Status != securities.OrderPartiallyFilled {
		t.Fatalf("unexpected sell partial fill: %+v, %v", sell, err)
	}
	afterSell, err := service.Portfolio(t.Context(), registration.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if afterSell.Cash.Settled.String() != "99714.88" || afterSell.Cash.Withdrawable.String() != "99714.88" ||
		afterSell.Cash.ProvisionalBuyingPower.String() != "47.48" || afterSell.Cash.TotalBuyingPower.String() != "99762.36" {
		t.Fatalf("provisional, settled, and withdrawable cash were not separated: %+v", afterSell.Cash)
	}

	if _, err := service.SubmitOrder(t.Context(), securities.SubmitOrderCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-view-only-0001",
		Symbol:         "FIXADR",
		Side:           broker.SideBuy,
		OrderType:      broker.OrderTypeMarket,
		TimeInForce:    broker.TimeInForceDay,
		Quantity:       money.MustParse("1"),
	}); !errors.Is(err, securities.ErrInstrumentViewOnly) {
		t.Fatalf("expected view-only rejection, got %v", err)
	}
	if _, err := service.Portfolio(t.Context(), "invalid-token"); !errors.Is(err, securities.ErrUnauthorized) {
		t.Fatalf("expected authorization failure, got %v", err)
	}

	assertCount(t, pool, 3, "SELECT COUNT(*) FROM securities.fills")
	assertCount(t, pool, 3, "SELECT COUNT(*) FROM ledger.transactions WHERE transaction_type = 'PAPER_SECURITY_FILL'")
	assertCount(t, pool, 0, `
		SELECT COUNT(*) FROM (
			SELECT transaction_id, currency
			FROM ledger.entries
			GROUP BY transaction_id, currency
			HAVING SUM(amount) FILTER (WHERE direction = 'DEBIT') <> SUM(amount) FILTER (WHERE direction = 'CREDIT')
		) AS unbalanced`)
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM audit.events WHERE action LIKE 'paper.%' AND actor_id = ''")
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM outbox.events WHERE aggregate_type LIKE 'paper.%' AND payload = '{}'::JSONB")
}

func TestPaperOrderConcurrentIdempotency(t *testing.T) {
	pool := newFinancialTestPool(t)
	service := newPaperService(t, pool, nil, 0)
	registration, err := service.RegisterFixture(t.Context(), "paper.concurrent")
	if err != nil {
		t.Fatal(err)
	}
	command := securities.SubmitOrderCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-concurrent-order-0001",
		Symbol:         "SPY",
		Side:           broker.SideBuy,
		OrderType:      broker.OrderTypeMarket,
		TimeInForce:    broker.TimeInForceDay,
		Quantity:       money.MustParse("0.25"),
	}

	const callers = 8
	ids := make(chan string, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			order, submitErr := service.SubmitOrder(t.Context(), command)
			if submitErr != nil {
				errorsFound <- submitErr
				return
			}
			ids <- order.ID
		}()
	}
	wait.Wait()
	close(ids)
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent order failed: %v", err)
	}
	identifier := ""
	for id := range ids {
		if identifier == "" {
			identifier = id
		}
		if id != identifier {
			t.Fatalf("expected one order, got %s and %s", identifier, id)
		}
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM securities.orders")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM securities.fills")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM ledger.transactions WHERE transaction_type = 'PAPER_SECURITY_FILL'")
}

func TestPaperProviderTimeoutRollsBackOrder(t *testing.T) {
	pool := newFinancialTestPool(t)
	service := newPaperService(t, pool, timeoutBroker{}, 10*time.Millisecond)
	registration, err := service.RegisterFixture(t.Context(), "paper.provider-timeout")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SubmitOrder(t.Context(), securities.SubmitOrderCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-timeout-order-0001",
		Symbol:         "AAPL",
		Side:           broker.SideBuy,
		OrderType:      broker.OrderTypeMarket,
		TimeInForce:    broker.TimeInForceDay,
		Quantity:       money.MustParse("1"),
	})
	if !errors.Is(err, broker.ErrProviderTimeout) {
		t.Fatalf("expected provider timeout, got %v", err)
	}
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM securities.orders")
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM securities.order_requests")
}

func newPaperService(t *testing.T, pool *pgxpool.Pool, adapter broker.Adapter, timeout time.Duration) *securities.Service {
	t.Helper()
	quoteProvider, err := marketprovider.NewLocal(pool, config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	if adapter == nil {
		adapter, err = brokerprovider.NewLocal(config.EnvironmentTest)
		if err != nil {
			t.Fatal(err)
		}
	}
	service, err := securities.NewService(securities.Dependencies{
		Database:        pool,
		Environment:     config.EnvironmentTest,
		Catalog:         marketdata.NewCatalog(pool),
		Quotes:          quoteProvider,
		Broker:          adapter,
		ProviderTimeout: timeout,
		Now: func() time.Time {
			return time.Date(2026, time.July, 19, 14, 30, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type timeoutBroker struct{}

func (timeoutBroker) Name() string { return "timeout-paper-broker" }
func (timeoutBroker) Capabilities(context.Context) broker.Capabilities {
	return broker.Capabilities{Mode: "SIMULATED"}
}
func (timeoutBroker) Health(context.Context) error { return nil }
func (timeoutBroker) Execute(ctx context.Context, _ broker.Request) (broker.Event, error) {
	<-ctx.Done()
	return broker.Event{}, ctx.Err()
}
