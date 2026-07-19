//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/foundation/migrations"
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	marketprovider "github.com/KDTikkly/Cytisus/internal/marketdata/provider"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/paperapi"
	"github.com/KDTikkly/Cytisus/internal/securities"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
	brokerprovider "github.com/KDTikkly/Cytisus/internal/securities/provider"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPaperSecuritiesMigrationUpDownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), financialTestTimeout)
	defer cancel()
	databaseURL := requiredEnv(t, "DATABASE_URL")
	directory := filepath.Join("..", "..", "db", "migrations")
	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatal(err)
	}
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)

	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000004_paper_order_actions.down.sql"))
	if _, err := connection.Exec(ctx, "DELETE FROM public.cytisus_schema_migrations WHERE version = $1", "000004_paper_order_actions"); err != nil {
		t.Fatal(err)
	}
	assertRelationExists(t, ctx, connection, "securities.order_action_requests", false)
	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatalf("reapply paper action migration: %v", err)
	}
	assertRelationExists(t, ctx, connection, "securities.order_action_requests", true)

	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000008_rwa_simulator.down.sql"))
	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000007_card_mvp.down.sql"))
	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000006_crypto_routing.down.sql"))
	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000005_banking_compliance.down.sql"))
	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000004_paper_order_actions.down.sql"))
	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000003_paper_securities.down.sql"))
	if _, err := connection.Exec(ctx, `
		DELETE FROM public.cytisus_schema_migrations
		WHERE version IN (
			'000003_paper_securities', '000004_paper_order_actions',
			'000005_banking_compliance', '000006_crypto_routing',
			'000007_card_mvp', '000008_rwa_simulator'
		)`); err != nil {
		t.Fatal(err)
	}
	assertNamedSchemaExists(t, ctx, connection, "marketdata", false)
	assertNamedSchemaExists(t, ctx, connection, "securities", false)
	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatalf("reapply paper securities migrations: %v", err)
	}
	assertRelationExists(t, ctx, connection, "marketdata.quote_fixtures", true)
	assertRelationExists(t, ctx, connection, "securities.orders", true)
	assertRelationExists(t, ctx, connection, "securities.order_action_requests", true)
	assertRelationExists(t, ctx, connection, "securities.position_reservations", true)
	var instrumentCount int64
	if err := connection.QueryRow(ctx, "SELECT COUNT(*) FROM marketdata.instruments").Scan(&instrumentCount); err != nil {
		t.Fatal(err)
	}
	if instrumentCount != 12 {
		t.Fatalf("expected twelve instrument fixtures after migration replay, got %d", instrumentCount)
	}
}

func TestPaperAPIEndToEnd(t *testing.T) {
	pool := newFinancialTestPool(t)
	server := httptest.NewServer(paperapi.New(newPaperService(t, pool, nil, 0), ""))
	t.Cleanup(server.Close)

	registration := apiRequest(t, http.MethodPost, server.URL+"/v1/paper/registrations", "", "", map[string]string{
		"fixture_id": "paper.api-e2e",
	}, http.StatusCreated)
	token := registration["access_token"].(string)
	if registration["mode"] != "SIMULATED" {
		t.Fatalf("registration did not identify simulator mode: %+v", registration)
	}

	search := apiRequest(t, http.MethodGet, server.URL+"/v1/instruments?q=AAPL", "", "", nil, http.StatusOK)
	if len(search["items"].([]any)) != 1 {
		t.Fatalf("expected one exact instrument result: %+v", search)
	}
	order := apiRequest(t, http.MethodPost, server.URL+"/v1/orders", token, "api-order-00000001", map[string]string{
		"symbol":        "AAPL",
		"side":          "BUY",
		"order_type":    "MARKET",
		"time_in_force": "DAY",
		"quantity":      "0.5",
	}, http.StatusCreated)
	if order["status"] != "PARTIALLY_FILLED" || order["quote_status"] != "SIMULATED" {
		t.Fatalf("unexpected API order: %+v", order)
	}
	orderID := order["id"].(string)
	completed := apiRequest(t, http.MethodPost, server.URL+"/v1/orders/"+orderID+"/replay", token, "api-replay-0000001", nil, http.StatusOK)
	if completed["status"] != "FILLED" {
		t.Fatalf("expected deterministic replay fill: %+v", completed)
	}
	portfolio := apiRequest(t, http.MethodGet, server.URL+"/v1/portfolio", token, "", nil, http.StatusOK)
	cash := portfolio["cash"].(map[string]any)
	if cash["settled"] == cash["provisional_buying_power"] || cash["withdrawable"] != cash["settled"] {
		t.Fatalf("cash dimensions were not represented separately: %+v", cash)
	}
	unauthorized := apiRequest(t, http.MethodGet, server.URL+"/v1/portfolio", "", "", nil, http.StatusUnauthorized)
	if unauthorized["error"].(map[string]any)["code"] != "AUTHENTICATION_REQUIRED" {
		t.Fatalf("unexpected stable authorization error: %+v", unauthorized)
	}
}

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

func TestPaperLimitDayGTCAndCancellation(t *testing.T) {
	pool := newFinancialTestPool(t)
	service := newPaperService(t, pool, nil, 0)
	registration, err := service.RegisterFixture(t.Context(), "paper.limit-orders")
	if err != nil {
		t.Fatal(err)
	}
	dayLimit := money.MustParse("100")
	dayOrder, err := service.SubmitOrder(t.Context(), securities.SubmitOrderCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-limit-day-0001",
		Symbol:         "AAPL",
		Side:           broker.SideBuy,
		OrderType:      broker.OrderTypeLimit,
		TimeInForce:    broker.TimeInForceDay,
		Quantity:       money.MustParse("1"),
		LimitPrice:     &dayLimit,
	})
	if err != nil || dayOrder.Status != securities.OrderOpen {
		t.Fatalf("expected non-marketable DAY limit to open, got %+v, %v", dayOrder, err)
	}
	dayOrder, err = service.AdvanceReplay(t.Context(), securities.ActionCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-limit-day-replay-0001",
		OrderID:        dayOrder.ID,
	})
	if err != nil || dayOrder.Status != securities.OrderOpen {
		t.Fatalf("expected DAY limit to remain open at cursor one, got %+v, %v", dayOrder, err)
	}
	dayOrder, err = service.AdvanceReplay(t.Context(), securities.ActionCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-limit-day-replay-0002",
		OrderID:        dayOrder.ID,
	})
	if err != nil || dayOrder.Status != securities.OrderExpired {
		t.Fatalf("expected DAY limit to expire deterministically, got %+v, %v", dayOrder, err)
	}

	gtcLimit := money.MustParse("190.08")
	gtcOrder, err := service.SubmitOrder(t.Context(), securities.SubmitOrderCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-limit-gtc-0001",
		Symbol:         "AAPL",
		Side:           broker.SideBuy,
		OrderType:      broker.OrderTypeLimit,
		TimeInForce:    broker.TimeInForceGTC,
		Quantity:       money.MustParse("0.4"),
		LimitPrice:     &gtcLimit,
	})
	if err != nil || gtcOrder.Status != securities.OrderOpen {
		t.Fatalf("expected non-marketable GTC limit to open, got %+v, %v", gtcOrder, err)
	}
	gtcOrder, err = service.AdvanceReplay(t.Context(), securities.ActionCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-limit-gtc-replay-0001",
		OrderID:        gtcOrder.ID,
	})
	if err != nil || gtcOrder.Status != securities.OrderPartiallyFilled || gtcOrder.FilledQuantity.String() != "0.2" {
		t.Fatalf("expected GTC limit partial fill at cursor one, got %+v, %v", gtcOrder, err)
	}
	cancelled, err := service.CancelOrder(t.Context(), securities.ActionCommand{
		AccessToken:    registration.AccessToken,
		IdempotencyKey: "paper-limit-gtc-cancel-0001",
		OrderID:        gtcOrder.ID,
	})
	if err != nil || cancelled.Status != securities.OrderCancelled {
		t.Fatalf("expected partially filled GTC cancellation, got %+v, %v", cancelled, err)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM securities.fills WHERE order_id = $1", gtcOrder.ID)
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

func apiRequest(t *testing.T, method, target, token, idempotencyKey string, body any, expectedStatus int) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, target, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoded := make(map[string]any)
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != expectedStatus {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, target, expectedStatus, response.StatusCode, fmt.Sprint(decoded))
	}
	return decoded
}

func applyMigrationFile(t *testing.T, ctx context.Context, connection *pgx.Conn, path string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, string(contents)); err != nil {
		t.Fatalf("apply %s: %v", filepath.Base(path), err)
	}
}

func assertRelationExists(t *testing.T, ctx context.Context, connection *pgx.Conn, name string, expected bool) {
	t.Helper()
	var exists bool
	if err := connection.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != expected {
		t.Fatalf("expected relation %s existence %t, got %t", name, expected, exists)
	}
}
