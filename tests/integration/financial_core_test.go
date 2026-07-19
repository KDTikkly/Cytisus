//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/migrations"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/outbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const financialTestTimeout = 60 * time.Second

func TestLedgerPostingIdempotencyProjectionAndConstraints(t *testing.T) {
	pool := newFinancialTestPool(t)
	service, assetAccount, clearingAccount := newLedgerFixture(t, pool)
	command := postingCommand(assetAccount.ID, clearingAccount.ID, "posting-request-0001", nil)

	posted, err := service.Post(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.Post(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.TransactionID != posted.TransactionID {
		t.Fatalf("expected request replay of %s, got %+v", posted.TransactionID, replayed)
	}

	conflict := command
	conflict.Entries = append([]ledger.Entry(nil), command.Entries...)
	conflict.Entries[0].Amount = money.MustParse("11")
	conflict.Entries[1].Amount = money.MustParse("11")
	if _, err := service.Post(t.Context(), conflict); !errors.Is(err, ledger.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}

	assetBalance, err := service.Balance(t.Context(), assetAccount.ID, money.MustParseCurrency("USD"), ledger.DimensionSettled)
	if err != nil {
		t.Fatal(err)
	}
	clearingBalance, err := service.Balance(t.Context(), clearingAccount.ID, money.MustParseCurrency("USD"), ledger.DimensionSettled)
	if err != nil {
		t.Fatal(err)
	}
	if assetBalance.String() != "10" || clearingBalance.String() != "-10" {
		t.Fatalf("unexpected balances: asset=%s clearing=%s", assetBalance, clearingBalance)
	}

	assertCount(t, pool, 1, "SELECT COUNT(*) FROM ledger.transactions WHERE id = $1", posted.TransactionID)
	assertCount(t, pool, 2, "SELECT COUNT(*) FROM ledger.entries WHERE transaction_id = $1", posted.TransactionID)
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM audit.events WHERE correlation_id = $1", posted.TransactionID)
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM outbox.events WHERE aggregate_id = $1", posted.TransactionID)

	_, err = pool.Exec(t.Context(), "UPDATE ledger.entries SET amount = 99 WHERE transaction_id = $1", posted.TransactionID)
	assertSQLState(t, err, "55000")
	_, err = pool.Exec(t.Context(), "UPDATE ledger.account_balances SET debit_balance = 99 WHERE account_id = $1", assetAccount.ID)
	if err == nil {
		t.Fatal("expected derived balance projection to reject direct updates")
	}

	assertDatabaseRejectsUnbalancedTransaction(t, pool, assetAccount.ID)
}

func TestProviderEventIdempotencyAndConflict(t *testing.T) {
	pool := newFinancialTestPool(t)
	service, assetAccount, clearingAccount := newLedgerFixture(t, pool)
	providerEvent := &ledger.ProviderEvent{
		Provider:        "bank-sim",
		ExternalEventID: "provider-event-0001",
		Payload:         []byte(`{"amount":"10","currency":"USD"}`),
	}
	original, err := service.Post(t.Context(), postingCommand(assetAccount.ID, clearingAccount.ID, "provider-request-0001", providerEvent))
	if err != nil {
		t.Fatal(err)
	}

	duplicate := postingCommand(assetAccount.ID, clearingAccount.ID, "provider-request-0002", providerEvent)
	replayed, err := service.Post(t.Context(), duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.TransactionID != original.TransactionID {
		t.Fatalf("expected provider replay of %s, got %+v", original.TransactionID, replayed)
	}

	changedEvent := *providerEvent
	changedEvent.Payload = []byte(`{"amount":"10.01","currency":"USD"}`)
	conflict := postingCommand(assetAccount.ID, clearingAccount.ID, "provider-request-0003", &changedEvent)
	if _, err := service.Post(t.Context(), conflict); !errors.Is(err, ledger.ErrProviderEventConflict) {
		t.Fatalf("expected provider event conflict, got %v", err)
	}

	assertCount(t, pool, 1, "SELECT COUNT(*) FROM ledger.transactions")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM ledger.provider_events WHERE provider = 'bank-sim' AND external_event_id = 'provider-event-0001'")
}

func TestLedgerConcurrentRequestIdempotency(t *testing.T) {
	pool := newFinancialTestPool(t)
	service, assetAccount, clearingAccount := newLedgerFixture(t, pool)
	command := postingCommand(assetAccount.ID, clearingAccount.ID, "concurrent-request-0001", nil)

	const callers = 16
	results := make(chan ledger.PostingResult, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			result, err := service.Post(t.Context(), command)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent post failed: %v", err)
	}
	if t.Failed() {
		return
	}

	transactionID := ""
	created := 0
	for result := range results {
		if transactionID == "" {
			transactionID = result.TransactionID
		}
		if result.TransactionID != transactionID {
			t.Fatalf("expected one transaction ID, got %s and %s", transactionID, result.TransactionID)
		}
		if !result.Replayed {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly one creator, got %d", created)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM ledger.transactions")
	assertCount(t, pool, 2, "SELECT COUNT(*) FROM ledger.entries")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM audit.events WHERE correlation_id = $1", transactionID)
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM outbox.events WHERE aggregate_id = $1", transactionID)
}

func TestConcurrentReversalIsSingleAndRestoresProjection(t *testing.T) {
	pool := newFinancialTestPool(t)
	service, assetAccount, clearingAccount := newLedgerFixture(t, pool)
	posted, err := service.Post(t.Context(), postingCommand(assetAccount.ID, clearingAccount.ID, "reversal-source-0001", nil))
	if err != nil {
		t.Fatal(err)
	}

	const callers = 12
	type reversalOutcome struct {
		result ledger.PostingResult
		err    error
	}
	outcomes := make(chan reversalOutcome, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for index := range callers {
		go func() {
			defer wait.Done()
			result, reverseErr := service.Reverse(t.Context(), ledger.ReversalCommand{
				Scope:                 "ledger.reverse",
				IdempotencyKey:        fmt.Sprintf("reverse-request-%04d", index),
				OriginalTransactionID: posted.TransactionID,
				ReasonCode:            "OPERATOR_CORRECTION",
				PolicyVersion:         "ledger-v1",
				EffectiveAt:           time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC),
				Actor:                 ledger.Actor{Type: "ADMIN", ID: "ops-test"},
			})
			outcomes <- reversalOutcome{result: result, err: reverseErr}
		}()
	}
	wait.Wait()
	close(outcomes)

	succeeded := 0
	alreadyReversed := 0
	reversalID := ""
	for outcome := range outcomes {
		switch {
		case outcome.err == nil:
			succeeded++
			reversalID = outcome.result.TransactionID
		case errors.Is(outcome.err, ledger.ErrAlreadyReversed):
			alreadyReversed++
		default:
			t.Errorf("unexpected reversal result: %v", outcome.err)
		}
	}
	if succeeded != 1 || alreadyReversed != callers-1 {
		t.Fatalf("expected one reversal and %d conflicts, got success=%d conflicts=%d", callers-1, succeeded, alreadyReversed)
	}

	assetBalance, err := service.Balance(t.Context(), assetAccount.ID, money.MustParseCurrency("USD"), ledger.DimensionSettled)
	if err != nil {
		t.Fatal(err)
	}
	clearingBalance, err := service.Balance(t.Context(), clearingAccount.ID, money.MustParseCurrency("USD"), ledger.DimensionSettled)
	if err != nil {
		t.Fatal(err)
	}
	if !assetBalance.IsZero() || !clearingBalance.IsZero() {
		t.Fatalf("reversal did not restore balances: asset=%s clearing=%s", assetBalance, clearingBalance)
	}
	assertCount(t, pool, 2, "SELECT COUNT(*) FROM ledger.transactions")
	assertCount(t, pool, 4, "SELECT COUNT(*) FROM ledger.entries")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM ledger.reversals")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM audit.events WHERE correlation_id = $1", reversalID)
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM outbox.events WHERE aggregate_id = $1", reversalID)
}

func TestOutboxRetryDLQReplayAndConsumerIdempotency(t *testing.T) {
	pool := newFinancialTestPool(t)
	createOutboxEffectTable(t, pool)
	eventID := insertOutboxEvent(t, pool, "retry-event", 2)
	worker := newTestWorker(t, pool, "worker.retry", 1)
	failingHandler := &effectHandler{name: "ledger.projector", fail: true}

	summary, err := worker.RunOnce(t.Context(), failingHandler)
	if err == nil || summary.Retried != 1 {
		t.Fatalf("expected first attempt to retry, summary=%+v err=%v", summary, err)
	}
	assertOutboxState(t, pool, eventID, outbox.StatusPending, 1, "provider.timeout")
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM outbox.consumer_receipts WHERE event_id = $1", eventID)

	if _, err := pool.Exec(t.Context(), "UPDATE outbox.events SET available_at = clock_timestamp() WHERE id = $1", eventID); err != nil {
		t.Fatal(err)
	}
	summary, err = worker.RunOnce(t.Context(), failingHandler)
	if err == nil || summary.DeadLettered != 1 {
		t.Fatalf("expected second attempt to dead-letter, summary=%+v err=%v", summary, err)
	}
	assertOutboxState(t, pool, eventID, outbox.StatusDeadLetter, 2, "provider.timeout")

	admin := outbox.NewAdminService(pool)
	replayed, err := admin.Replay(t.Context(), eventID, "ops-test", "MANUAL_RETRY")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Status != outbox.StatusPending || replayed.Attempt != 0 {
		t.Fatalf("unexpected replayed event: %+v", replayed)
	}

	successHandler := &effectHandler{name: "ledger.projector"}
	summary, err = worker.RunOnce(t.Context(), successHandler)
	if err != nil || summary.Delivered != 1 || summary.Deduplicated != 0 {
		t.Fatalf("expected successful delivery, summary=%+v err=%v", summary, err)
	}
	assertOutboxState(t, pool, eventID, outbox.StatusDelivered, 1, "")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM foundation.outbox_test_effects WHERE event_id = $1", eventID)
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM outbox.consumer_receipts WHERE consumer_name = 'ledger.projector' AND event_id = $1", eventID)

	if _, err := pool.Exec(t.Context(), `
		UPDATE outbox.events
		SET status = 'DEAD_LETTER', attempt_count = max_attempts, delivered_at = NULL, last_error = 'test.replay'
		WHERE id = $1`, eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Replay(t.Context(), eventID, "ops-test", "VERIFY_DEDUPLICATION"); err != nil {
		t.Fatal(err)
	}
	summary, err = worker.RunOnce(t.Context(), successHandler)
	if err != nil || summary.Delivered != 1 || summary.Deduplicated != 1 {
		t.Fatalf("expected deduplicated replay, summary=%+v err=%v", summary, err)
	}
	if successHandler.calls.Load() != 1 {
		t.Fatalf("expected consumer effect handler once, got %d", successHandler.calls.Load())
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM foundation.outbox_test_effects WHERE event_id = $1", eventID)
	assertCount(t, pool, 2, "SELECT COUNT(*) FROM audit.events WHERE action = 'outbox.event.replayed' AND correlation_id = $1", eventID)
}

func TestOutboxConcurrentClaimsLeaseRecoveryAndCancel(t *testing.T) {
	pool := newFinancialTestPool(t)
	createOutboxEffectTable(t, pool)
	for index := range 10 {
		insertOutboxEvent(t, pool, fmt.Sprintf("concurrent-%02d", index), 3)
	}

	first := newTestWorker(t, pool, "worker.claim-a", 5)
	second := newTestWorker(t, pool, "worker.claim-b", 5)
	handler := &effectHandler{name: "ledger.projector"}
	type workerResult struct {
		summary outbox.RunSummary
		err     error
	}
	results := make(chan workerResult, 2)
	for _, worker := range []*outbox.Worker{first, second} {
		go func() {
			summary, runErr := worker.RunOnce(t.Context(), handler)
			results <- workerResult{summary: summary, err: runErr}
		}()
	}
	delivered := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		delivered += result.summary.Delivered
	}
	if delivered != 10 {
		t.Fatalf("expected ten deliveries across workers, got %d", delivered)
	}
	assertCount(t, pool, 10, "SELECT COUNT(*) FROM outbox.events WHERE status = 'DELIVERED'")
	assertCount(t, pool, 10, "SELECT COUNT(*) FROM foundation.outbox_test_effects")

	leaseEventID := insertOutboxEvent(t, pool, "lease-recovery", 3)
	if _, err := pool.Exec(t.Context(), `
		UPDATE outbox.events
		SET status = 'PROCESSING', attempt_count = 1, claimed_by = 'crashed.worker', claimed_at = clock_timestamp() - interval '2 seconds'
		WHERE id = $1`, leaseEventID); err != nil {
		t.Fatal(err)
	}
	recovery := newTestWorker(t, pool, "worker.recovery", 1)
	summary, err := recovery.RunOnce(t.Context(), handler)
	if err != nil || summary.Delivered != 1 {
		t.Fatalf("expected stale lease recovery, summary=%+v err=%v", summary, err)
	}
	assertOutboxState(t, pool, leaseEventID, outbox.StatusDelivered, 2, "")

	exhaustedEventID := insertOutboxEvent(t, pool, "exhausted-lease", 1)
	if _, err := pool.Exec(t.Context(), `
		UPDATE outbox.events
		SET status = 'PROCESSING', attempt_count = max_attempts, claimed_by = 'crashed.worker', claimed_at = clock_timestamp() - interval '2 seconds'
		WHERE id = $1`, exhaustedEventID); err != nil {
		t.Fatal(err)
	}
	summary, err = recovery.RunOnce(t.Context(), handler)
	if err != nil || summary.Claimed != 0 || summary.DeadLettered != 1 {
		t.Fatalf("expected exhausted stale claim to enter DLQ, summary=%+v err=%v", summary, err)
	}
	assertOutboxState(t, pool, exhaustedEventID, outbox.StatusDeadLetter, 1, "worker.lease_expired")

	cancelEventID := insertOutboxEvent(t, pool, "cancel-event", 3)
	admin := outbox.NewAdminService(pool)
	cancelled, err := admin.Cancel(t.Context(), cancelEventID, "ops-test", "NO_LONGER_REQUIRED")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != outbox.StatusCancelled {
		t.Fatalf("expected cancelled status, got %+v", cancelled)
	}
	summary, err = recovery.RunOnce(t.Context(), handler)
	if err != nil || summary.Claimed != 0 {
		t.Fatalf("cancelled event was claimable, summary=%+v err=%v", summary, err)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM audit.events WHERE action = 'outbox.event.cancelled' AND correlation_id = $1", cancelEventID)
}

func TestLedgerMigrationUpDownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), financialTestTimeout)
	defer cancel()
	databaseURL := requiredEnv(t, "DATABASE_URL")
	directory := filepath.Join("..", "..", "db", "migrations")
	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	down, err := os.ReadFile(filepath.Join(directory, "000002_ledger_async_core.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply Ledger down migration: %v", err)
	}
	if _, err := conn.Exec(ctx, "DELETE FROM public.cytisus_schema_migrations WHERE version = $1", "000002_ledger_async_core"); err != nil {
		t.Fatal(err)
	}
	assertNamedSchemaExists(t, ctx, conn, "ledger", false)
	assertNamedSchemaExists(t, ctx, conn, "audit", false)
	assertNamedSchemaExists(t, ctx, conn, "outbox", false)

	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatalf("reapply Ledger migration: %v", err)
	}
	assertNamedSchemaExists(t, ctx, conn, "ledger", true)
	assertNamedSchemaExists(t, ctx, conn, "audit", true)
	assertNamedSchemaExists(t, ctx, conn, "outbox", true)
}

func newFinancialTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), financialTestTimeout)
	t.Cleanup(cancel)
	databaseURL := requiredEnv(t, "DATABASE_URL")
	if err := migrations.Run(ctx, databaseURL, filepath.Join("..", "..", "db", "migrations")); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `
		TRUNCATE TABLE
			outbox.consumer_receipts,
			outbox.events,
			audit.events,
			ledger.hold_events,
			ledger.holds,
			ledger.provider_events,
			ledger.request_idempotency,
			ledger.reversals,
			ledger.entries,
			ledger.transactions,
			ledger.accounts
		CASCADE`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func newLedgerFixture(t *testing.T, pool *pgxpool.Pool) (*ledger.Service, ledger.Account, ledger.Account) {
	t.Helper()
	service := ledger.NewService(pool)
	actor := ledger.Actor{Type: "SYSTEM", ID: "integration-test"}
	asset, err := service.OpenAccount(t.Context(), ledger.AccountCommand{
		AccountKey:  "test.user-cash",
		OwnerType:   "USER",
		OwnerID:     "synthetic-user-001",
		AccountType: "CASH",
		Currency:    money.MustParseCurrency("USD"),
		NormalSide:  ledger.Debit,
		Actor:       actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	clearing, err := service.OpenAccount(t.Context(), ledger.AccountCommand{
		AccountKey:  "test.provider-clearing",
		OwnerType:   "PROVIDER",
		OwnerID:     "synthetic-provider-001",
		AccountType: "PROVIDER_CLEARING",
		Currency:    money.MustParseCurrency("USD"),
		NormalSide:  ledger.Credit,
		Actor:       actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, asset, clearing
}

func postingCommand(assetAccountID, clearingAccountID, idempotencyKey string, providerEvent *ledger.ProviderEvent) ledger.PostingCommand {
	return ledger.PostingCommand{
		Scope:           "ledger.post",
		IdempotencyKey:  idempotencyKey,
		TransactionType: "CASH_TRANSFER",
		PolicyVersion:   "ledger-v1",
		EffectiveAt:     time.Date(2026, time.July, 19, 0, 0, 0, 0, time.UTC),
		Actor:           ledger.Actor{Type: "SYSTEM", ID: "integration-test"},
		Entries: []ledger.Entry{
			{AccountID: assetAccountID, Currency: money.MustParseCurrency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: money.MustParse("10")},
			{AccountID: clearingAccountID, Currency: money.MustParseCurrency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: money.MustParse("10")},
		},
		ProviderEvent: providerEvent,
	}
}

func assertDatabaseRejectsUnbalancedTransaction(t *testing.T, pool *pgxpool.Pool, accountID string) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	var transactionID string
	if err := tx.QueryRow(t.Context(), "SELECT gen_random_uuid()::TEXT").Scan(&transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `
		INSERT INTO ledger.transactions (id, transaction_type, policy_version, effective_at)
		VALUES ($1, 'INVALID_TEST', 'ledger-v1', clock_timestamp())`, transactionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `
		INSERT INTO ledger.entries (
			transaction_id, entry_sequence, account_id, currency, balance_dimension, direction, amount
		) VALUES ($1, 1, $2, 'USD', 'SETTLED', 'DEBIT', 1)`, transactionID, accountID); err != nil {
		t.Fatal(err)
	}
	assertSQLState(t, tx.Commit(t.Context()), "23514")
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM ledger.transactions WHERE id = $1", transactionID)
}

func assertSQLState(t *testing.T, err error, expected string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != expected {
		t.Fatalf("expected PostgreSQL state %s, got %v", expected, err)
	}
}

func assertCount(t *testing.T, pool *pgxpool.Pool, expected int64, query string, arguments ...any) {
	t.Helper()
	var actual int64
	if err := pool.QueryRow(t.Context(), query, arguments...).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("expected count %d, got %d for %s", expected, actual, query)
	}
}

type safeProviderTimeout struct{}

func (safeProviderTimeout) Error() string    { return "synthetic sensitive provider timeout detail" }
func (safeProviderTimeout) SafeCode() string { return "provider.timeout" }

type effectHandler struct {
	name  string
	fail  bool
	calls atomic.Int32
}

func (handler *effectHandler) ConsumerName() string { return handler.name }

func (handler *effectHandler) Handle(ctx context.Context, tx pgx.Tx, event outbox.Event) error {
	handler.calls.Add(1)
	if handler.fail {
		return safeProviderTimeout{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO foundation.outbox_test_effects (event_id, event_type)
		VALUES ($1, $2)`, event.ID, event.Type)
	return err
}

func createOutboxEffectTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `
		CREATE TABLE IF NOT EXISTS foundation.outbox_test_effects (
			event_id UUID PRIMARY KEY,
			event_type TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
		)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "TRUNCATE foundation.outbox_test_effects"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS foundation.outbox_test_effects"); err != nil {
			t.Errorf("drop outbox test effect table: %v", err)
		}
	})
}

func insertOutboxEvent(t *testing.T, pool *pgxpool.Pool, aggregateID string, maximumAttempts int32) string {
	t.Helper()
	var eventID string
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO outbox.events (
			aggregate_type, aggregate_id, event_type, event_version, payload, max_attempts
		) VALUES ('integration.fixture', $1, 'integration.event.created', 1, '{"synthetic":true}'::JSONB, $2)
		RETURNING id::TEXT`, aggregateID, maximumAttempts).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	return eventID
}

func newTestWorker(t *testing.T, pool *pgxpool.Pool, workerID string, batchSize int32) *outbox.Worker {
	t.Helper()
	worker, err := outbox.NewWorker(pool, outbox.Config{
		WorkerID:       workerID,
		BatchSize:      batchSize,
		LeaseDuration:  time.Second,
		BaseBackoff:    time.Second,
		MaximumBackoff: 4 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func assertOutboxState(t *testing.T, pool *pgxpool.Pool, eventID string, expectedStatus outbox.Status, expectedAttempt int32, expectedErrorCode string) {
	t.Helper()
	var status string
	var attempt int32
	var errorCode *string
	if err := pool.QueryRow(t.Context(), `
		SELECT status, attempt_count, last_error
		FROM outbox.events
		WHERE id = $1`, eventID).Scan(&status, &attempt, &errorCode); err != nil {
		t.Fatal(err)
	}
	actualErrorCode := ""
	if errorCode != nil {
		actualErrorCode = *errorCode
	}
	if outbox.Status(status) != expectedStatus || attempt != expectedAttempt || actualErrorCode != expectedErrorCode {
		t.Fatalf("unexpected outbox state: status=%s attempt=%d error=%q", status, attempt, actualErrorCode)
	}
}

func assertNamedSchemaExists(t *testing.T, ctx context.Context, conn *pgx.Conn, schema string, expected bool) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)", schema).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != expected {
		t.Fatalf("expected schema %s existence %t, got %t", schema, expected, exists)
	}
}
