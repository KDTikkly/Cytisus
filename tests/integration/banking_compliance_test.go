//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/banking"
	"github.com/KDTikkly/Cytisus/internal/banking/provider"
	"github.com/KDTikkly/Cytisus/internal/bankingapi"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/foundation/migrations"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBankingComplianceMigrationUpDownUp(t *testing.T) {
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

	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000005_banking_compliance.down.sql"))
	if _, err := connection.Exec(ctx, "DELETE FROM public.cytisus_schema_migrations WHERE version = $1", "000005_banking_compliance"); err != nil {
		t.Fatal(err)
	}
	assertNamedSchemaExists(t, ctx, connection, "banking", false)
	assertNamedSchemaExists(t, ctx, connection, "compliance", false)
	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatalf("reapply banking migration: %v", err)
	}
	assertRelationExists(t, ctx, connection, "banking.withdrawals", true)
	assertRelationExists(t, ctx, connection, "compliance.review_proposals", true)
}

func TestBankingAPIEndToEnd(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService := newPaperService(t, pool, nil, 0)
	registration, err := paperService.RegisterFixture(t.Context(), "banking.api-e2e")
	if err != nil {
		t.Fatal(err)
	}
	service := newBankingService(t, pool, paperService, nil)
	mux := http.NewServeMux()
	userHandler := bankingapi.NewUser(service, config.EnvironmentTest, "")
	mux.Handle("/v1/banks/", userHandler)
	mux.Handle("/v1/transfers/", userHandler)
	mux.Handle("/internal/v1/simulators/bank/", bankingapi.NewSimulator(service, config.EnvironmentTest))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	unauthorized := apiRequest(t, http.MethodGet, server.URL+"/v1/banks/accounts", "", "", nil, http.StatusUnauthorized)
	unauthorizedError := unauthorized["error"].(map[string]any)
	if unauthorizedError["code"] != "AUTHENTICATION_REQUIRED" || unauthorizedError["next_action"] == "" {
		t.Fatalf("banking error did not explain next action: %+v", unauthorized)
	}
	account := apiRequest(t, http.MethodPost, server.URL+"/v1/banks/accounts", registration.AccessToken, "", map[string]string{
		"external_account_reference": "fixture-api-ach", "rail_support": "ACH", "owner_relation": "SAME_NAME", "risk_class": "STANDARD",
	}, http.StatusCreated)
	accountID := account["id"].(string)
	verified := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/bank/events", "", "", map[string]string{
		"mode": "SIMULATED", "external_event_id": "api-ownership-0001", "resource_type": "BANK_ACCOUNT",
		"resource_id": accountID, "event_type": "OWNERSHIP_VERIFIED", "reason_code": "OWNERSHIP_CONFIRMED",
	}, http.StatusOK)
	if verified["ownership_status"] != "VERIFIED" || verified["mode"] != "SIMULATED" {
		t.Fatalf("unexpected API ownership response: %+v", verified)
	}
	fundingRequest := "phase4-funding-request-0001"
	funding := apiRequest(t, http.MethodPost, server.URL+"/v1/transfers/funding", registration.AccessToken, fundingRequest, map[string]string{
		"bank_account_id": accountID, "rail": "ACH", "amount": "125.50",
	}, http.StatusCreated)
	fundingID := funding["id"].(string)
	funding = apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/bank/events", "", "", map[string]string{
		"mode": "SIMULATED", "external_event_id": "api-ach-settle-0001", "resource_type": "FUNDING",
		"resource_id": fundingID, "event_type": "ACH_SETTLED", "reason_code": "SETTLED",
	}, http.StatusOK)
	if funding["status"] != "SETTLED" || funding["settled"] != true {
		t.Fatalf("unexpected API funding settlement: %+v", funding)
	}
	withdrawalRequest := "phase4-withdrawal-request-0001"
	withdrawal := apiRequest(t, http.MethodPost, server.URL+"/v1/transfers/withdrawals", registration.AccessToken, withdrawalRequest, map[string]string{
		"amount": "25.50",
	}, http.StatusCreated)
	if withdrawal["status"] != "APPROVED" || withdrawal["bank_account_id"] != accountID || withdrawal["next_action"] == "" {
		t.Fatalf("unexpected API closed-loop withdrawal: %+v", withdrawal)
	}
}

func TestACHWireWithdrawalComplianceAndReconciliation(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService := newPaperService(t, pool, nil, 0)
	registration, err := paperService.RegisterFixture(t.Context(), "banking.vertical-slice")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	service := newBankingService(t, pool, paperService, func() time.Time { return now })

	achAccount, err := service.LinkBankAccount(t.Context(), banking.LinkBankAccountCommand{
		AccessToken: registration.AccessToken, ExternalAccountReference: "fixture-ach-primary",
		RailSupport: "ACH", OwnerRelation: "SAME_NAME", RiskClass: "STANDARD",
	})
	if err != nil || achAccount.OwnershipStatus != "PENDING" {
		t.Fatalf("unexpected linked ACH account: %+v %v", achAccount, err)
	}
	achAccount, replayed, err := service.VerifyBankAccountOwnership(t.Context(), bankEvent(
		"ownership-ach-0001", "BANK_ACCOUNT", achAccount.ID, "OWNERSHIP_VERIFIED", "OWNERSHIP_CONFIRMED",
	))
	if err != nil || replayed || achAccount.OwnershipStatus != "VERIFIED" || achAccount.CoolingUntil == nil {
		t.Fatalf("unexpected ownership result: %+v replayed=%t err=%v", achAccount, replayed, err)
	}
	_, replayed, err = service.VerifyBankAccountOwnership(t.Context(), bankEvent(
		"ownership-ach-0001", "BANK_ACCOUNT", achAccount.ID, "OWNERSHIP_VERIFIED", "OWNERSHIP_CONFIRMED",
	))
	if err != nil || !replayed {
		t.Fatalf("expected ownership callback replay, replayed=%t err=%v", replayed, err)
	}

	ach, err := service.InitiateFunding(t.Context(), banking.InitiateFundingCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "ach-funding-request-0001",
		BankAccountID: achAccount.ID, Rail: "ACH", Amount: money.MustParse("1000"),
	})
	if err != nil || ach.Status != "INITIATED" || !ach.Pending {
		t.Fatalf("unexpected ACH initiation: %+v %v", ach, err)
	}
	assertCashDimensions(t, paperService, registration.AccessToken, "100000", "100000", "1000", "0")
	if _, err := service.ApplyFundingEvent(t.Context(), bankEvent("ach-processing-0001", "FUNDING", ach.ID, "ACH_PROCESSING", "PROCESSING")); err != nil {
		t.Fatal(err)
	}
	ach, err = service.ApplyFundingEvent(t.Context(), bankEvent("ach-settled-0001", "FUNDING", ach.ID, "ACH_SETTLED", "SETTLED"))
	if err != nil || ach.Status != "SETTLED" {
		t.Fatalf("unexpected ACH settlement: %+v %v", ach, err)
	}
	replayedACH, err := service.ApplyFundingEvent(t.Context(), bankEvent("ach-settled-0001", "FUNDING", ach.ID, "ACH_SETTLED", "SETTLED"))
	if err != nil || !replayedACH.Replayed || replayedACH.ID != ach.ID {
		t.Fatalf("expected duplicate ACH callback replay: %+v %v", replayedACH, err)
	}
	assertCashDimensions(t, paperService, registration.AccessToken, "101000", "101000", "0", "0")
	ach, err = service.ApplyFundingEvent(t.Context(), bankEvent("ach-return-0001", "FUNDING", ach.ID, "ACH_RETURNED", "NSF_RETURN"))
	if err != nil || ach.Status != "RETURNED" {
		t.Fatalf("unexpected ACH return: %+v %v", ach, err)
	}
	assertCashDimensions(t, paperService, registration.AccessToken, "100000", "100000", "0", "0")

	wireAccount, err := service.LinkBankAccount(t.Context(), banking.LinkBankAccountCommand{
		AccessToken: registration.AccessToken, ExternalAccountReference: "fixture-wire-mismatch",
		RailSupport: "WIRE", OwnerRelation: "THIRD_PARTY", RiskClass: "ELEVATED",
	})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := service.InitiateFunding(t.Context(), banking.InitiateFundingCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "wire-funding-request-0001",
		BankAccountID: wireAccount.ID, Rail: "WIRE", Amount: money.MustParse("2500"),
	})
	if err != nil || wire.Status != "INSTRUCTIONS_ISSUED" {
		t.Fatalf("unexpected Wire instructions: %+v %v", wire, err)
	}
	if _, err := service.ApplyFundingEvent(t.Context(), bankEvent("wire-detected-0001", "FUNDING", wire.ID, "WIRE_FUNDS_DETECTED", "FUNDS_DETECTED")); err != nil {
		t.Fatal(err)
	}
	wire, err = service.ApplyFundingEvent(t.Context(), bankEvent("wire-mismatch-0001", "FUNDING", wire.ID, "WIRE_NAME_MISMATCH", "NAME_MISMATCH"))
	if err != nil || wire.Status != "NAME_MISMATCH" || wire.Settled {
		t.Fatalf("unexpected Wire mismatch: %+v %v", wire, err)
	}
	assertCashDimensions(t, paperService, registration.AccessToken, "100000", "100000", "0", "0")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM compliance.cases WHERE resource_type = 'bank.funding' AND reason_code = 'NAME_MISMATCH'")

	ach2, err := service.InitiateFunding(t.Context(), banking.InitiateFundingCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "ach-funding-request-0002",
		BankAccountID: achAccount.ID, Rail: "ACH", Amount: money.MustParse("500"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ach2, err = service.ApplyFundingEvent(t.Context(), bankEvent("ach-settled-0002", "FUNDING", ach2.ID, "ACH_SETTLED", "SETTLED"))
	if err != nil || ach2.Status != "SETTLED" {
		t.Fatalf("unexpected second ACH settlement: %+v %v", ach2, err)
	}
	accounts, err := service.ListBankAccounts(t.Context(), registration.AccessToken)
	if err != nil || len(accounts) != 2 || !accounts[0].PreferredForWithdrawal || accounts[0].ID != achAccount.ID {
		t.Fatalf("funded same-name account was not preferred: %+v %v", accounts, err)
	}

	withdrawal, err := service.RequestWithdrawal(t.Context(), banking.RequestWithdrawalCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "bank-withdrawal-request-0001",
		Amount: money.MustParse("200"),
	})
	if err != nil || withdrawal.Status != "APPROVED" || withdrawal.BankAccountID != achAccount.ID {
		t.Fatalf("unexpected closed-loop withdrawal approval: %+v %v", withdrawal, err)
	}
	assertCashDimensions(t, paperService, registration.AccessToken, "100500", "100300", "0", "200")
	withdrawal, err = service.ApplyWithdrawalEvent(t.Context(), bankEvent("withdrawal-submit-0001", "WITHDRAWAL", withdrawal.ID, "WITHDRAWAL_SUBMITTED", "SUBMITTED"))
	if err != nil || withdrawal.Status != "SUBMITTED_TO_BANK" {
		t.Fatalf("unexpected withdrawal submission: %+v %v", withdrawal, err)
	}
	replayedWithdrawal, err := service.ApplyWithdrawalEvent(t.Context(), bankEvent("withdrawal-submit-0001", "WITHDRAWAL", withdrawal.ID, "WITHDRAWAL_SUBMITTED", "SUBMITTED"))
	if err != nil || !replayedWithdrawal.Replayed {
		t.Fatalf("expected withdrawal callback replay: %+v %v", replayedWithdrawal, err)
	}
	if _, err := service.ApplyWithdrawalEvent(t.Context(), bankEvent("withdrawal-processing-0001", "WITHDRAWAL", withdrawal.ID, "WITHDRAWAL_PROCESSING", "PROCESSING")); err != nil {
		t.Fatal(err)
	}
	withdrawal, err = service.ApplyWithdrawalEvent(t.Context(), bankEvent("withdrawal-settled-0001", "WITHDRAWAL", withdrawal.ID, "WITHDRAWAL_SETTLED", "SETTLED"))
	if err != nil || withdrawal.Status != "SETTLED" {
		t.Fatalf("unexpected withdrawal settlement: %+v %v", withdrawal, err)
	}
	assertCashDimensions(t, paperService, registration.AccessToken, "100300", "100300", "0", "0")

	run, err := service.ReconcileCustomer(t.Context(), banking.ReconcileCustomerCommand{
		Actor:             banking.AdminActor{ID: "finance.fixture", Role: "FINANCE"},
		CustomerReference: registration.Account.CustomerReference,
	})
	if err != nil || run.Status != "COMPLETED_WITHOUT_DIFFERENCE" || run.Difference.String() != "0" || run.LedgerAmount.String() != "300" {
		t.Fatalf("unexpected matched reconciliation: %+v %v", run, err)
	}
	assertCount(t, pool, 0, `
		SELECT COUNT(*) FROM (
			SELECT transaction_id, currency
			FROM ledger.entries
			GROUP BY transaction_id, currency
			HAVING SUM(amount) FILTER (WHERE direction = 'DEBIT') <> SUM(amount) FILTER (WHERE direction = 'CREDIT')
		) AS unbalanced`)
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM audit.events WHERE action LIKE 'bank.%' AND actor_id = ''")
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM outbox.events WHERE aggregate_type LIKE 'bank.%' AND payload = '{}'::JSONB")
}

func TestNewSameNameWithdrawalRequiresCoolingEnhancedReviewAndMakerChecker(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService := newPaperService(t, pool, nil, 0)
	registration, err := paperService.RegisterFixture(t.Context(), "banking.enhanced-review")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	service := newBankingService(t, pool, paperService, func() time.Time { return now })
	account, err := service.LinkBankAccount(t.Context(), banking.LinkBankAccountCommand{
		AccessToken: registration.AccessToken, ExternalAccountReference: "fixture-new-elevated",
		RailSupport: "BOTH", OwnerRelation: "SAME_NAME", RiskClass: "ELEVATED",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, _, err = service.VerifyBankAccountOwnership(t.Context(), bankEvent("ownership-elevated-0001", "BANK_ACCOUNT", account.ID, "OWNERSHIP_VERIFIED", "OWNERSHIP_CONFIRMED"))
	if err != nil {
		t.Fatal(err)
	}
	withdrawal, err := service.RequestWithdrawal(t.Context(), banking.RequestWithdrawalCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "enhanced-withdrawal-0001",
		BankAccountID: account.ID, Amount: money.MustParse("50"),
	})
	if err != nil || withdrawal.Status != "COOLING_OFF" || withdrawal.ComplianceCaseID == "" || withdrawal.CoolingUntil == nil {
		t.Fatalf("new same-name account bypassed review: %+v %v", withdrawal, err)
	}
	proposal, err := service.ProposeReview(t.Context(), banking.ProposeReviewCommand{
		Actor: banking.AdminActor{ID: "maker.fixture", Role: "RISK_ANALYST"}, CaseID: withdrawal.ComplianceCaseID,
		Action: "OVERRIDE_COOLING", ReasonCode: "DOCUMENTED_EXCEPTION", TicketReference: "ticket-fixture-001", EvidenceReference: "evidence-fixture-001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecideReview(t.Context(), banking.DecideReviewCommand{
		Actor: banking.AdminActor{ID: "maker.fixture", Role: "RISK_ANALYST"}, ProposalID: proposal.ID, Approve: true, DecisionReason: "self approval attempt",
	}); !errors.Is(err, banking.ErrMakerCheckerConflict) {
		t.Fatalf("expected maker-checker conflict, got %v", err)
	}
	proposal, err = service.DecideReview(t.Context(), banking.DecideReviewCommand{
		Actor: banking.AdminActor{ID: "checker.fixture", Role: "COMPLIANCE_ANALYST"}, ProposalID: proposal.ID,
		Approve: true, DecisionReason: "Independent synthetic evidence review completed.",
	})
	if err != nil || proposal.Status != "APPROVED" {
		t.Fatalf("unexpected independent approval: %+v %v", proposal, err)
	}
	withdrawals, err := service.ListWithdrawals(t.Context(), registration.AccessToken, 20)
	if err != nil || len(withdrawals) != 1 || withdrawals[0].Status != "APPROVED" {
		t.Fatalf("override did not approve withdrawal: %+v %v", withdrawals, err)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM compliance.review_proposals WHERE case_id = $1 AND maker_id <> checker_id", withdrawal.ComplianceCaseID)
}

func TestThirdPartyWithdrawalProviderTimeoutConcurrencyAndDifference(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService := newPaperService(t, pool, nil, 0)
	registration, err := paperService.RegisterFixture(t.Context(), "banking.failures")
	if err != nil {
		t.Fatal(err)
	}
	service := newBankingService(t, pool, paperService, nil)
	thirdParty, err := service.LinkBankAccount(t.Context(), banking.LinkBankAccountCommand{
		AccessToken: registration.AccessToken, ExternalAccountReference: "fixture-third-party",
		RailSupport: "WIRE", OwnerRelation: "THIRD_PARTY", RiskClass: "ELEVATED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestWithdrawal(t.Context(), banking.RequestWithdrawalCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "third-party-withdrawal-0001",
		BankAccountID: thirdParty.ID, Amount: money.MustParse("10"),
	}); !errors.Is(err, banking.ErrThirdPartyDisabled) {
		t.Fatalf("expected permanent third-party rejection, got %v", err)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM banking.withdrawals WHERE status = 'REJECTED' AND reason_code = 'THIRD_PARTY_WITHDRAWAL_DISABLED'")

	timeoutAccount, err := service.LinkBankAccount(t.Context(), banking.LinkBankAccountCommand{
		AccessToken: registration.AccessToken, ExternalAccountReference: "fixture-timeout",
		RailSupport: "WIRE", OwnerRelation: "SAME_NAME", RiskClass: "STANDARD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.InitiateFunding(t.Context(), banking.InitiateFundingCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "timeout-funding-request-0001",
		BankAccountID: timeoutAccount.ID, Rail: "WIRE", Amount: money.MustParse("25"),
	}); !errors.Is(err, provider.ErrTimeout) {
		t.Fatalf("expected provider timeout, got %v", err)
	}
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM banking.funding_transfers")
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM banking.funding_requests")

	verified, err := service.LinkBankAccount(t.Context(), banking.LinkBankAccountCommand{
		AccessToken: registration.AccessToken, ExternalAccountReference: "fixture-concurrent-ach",
		RailSupport: "ACH", OwnerRelation: "SAME_NAME", RiskClass: "STANDARD",
	})
	if err != nil {
		t.Fatal(err)
	}
	verified, _, err = service.VerifyBankAccountOwnership(t.Context(), bankEvent("ownership-concurrent-0001", "BANK_ACCOUNT", verified.ID, "OWNERSHIP_VERIFIED", "OWNERSHIP_CONFIRMED"))
	if err != nil {
		t.Fatal(err)
	}
	command := banking.InitiateFundingCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "concurrent-ach-request-0001",
		BankAccountID: verified.ID, Rail: "ACH", Amount: money.MustParse("75"),
	}
	const callers = 8
	results := make(chan banking.FundingTransfer, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.InitiateFunding(t.Context(), command)
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
		t.Errorf("concurrent funding failed: %v", err)
	}
	identifier := ""
	for result := range results {
		if identifier == "" {
			identifier = result.ID
		}
		if result.ID != identifier {
			t.Fatalf("expected one funding transfer, got %s and %s", identifier, result.ID)
		}
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM banking.funding_transfers")

	providerAmount := money.MustParse("1")
	run, err := service.ReconcileCustomer(t.Context(), banking.ReconcileCustomerCommand{
		Actor: banking.AdminActor{ID: "finance.fixture", Role: "FINANCE"}, CustomerReference: registration.Account.CustomerReference,
		ProviderAmount: &providerAmount,
	})
	if !errors.Is(err, banking.ErrReconciliationDifference) || run.Status != "COMPLETED_WITH_DIFFERENCES" || run.CaseID == "" {
		t.Fatalf("expected visible reconciliation difference: %+v %v", run, err)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM banking.reconciliation_items WHERE status = 'DIFFERENCE'")
}

type bankingSessionResolver struct {
	service *securities.Service
}

func (resolver bankingSessionResolver) ResolveSession(ctx context.Context, token string) (banking.CustomerSession, error) {
	account, err := resolver.service.ResolveSession(ctx, token)
	if err != nil {
		return banking.CustomerSession{}, err
	}
	return banking.CustomerSession{
		PaperAccountID: account.ID, CustomerReference: account.CustomerReference, CashLedgerAccountID: account.CashLedgerAccountID,
	}, nil
}

func newBankingService(t *testing.T, pool *pgxpool.Pool, paperService *securities.Service, now func() time.Time) *banking.Service {
	t.Helper()
	adapter, err := provider.NewLocal(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	service, err := banking.NewService(banking.Dependencies{
		Database: pool, Environment: config.EnvironmentTest, Resolver: bankingSessionResolver{service: paperService}, Provider: adapter, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func bankEvent(externalID, resourceType, resourceID, eventType, reasonCode string) provider.Event {
	return provider.Event{
		Provider: "local-bank-simulator", ExternalEventID: externalID, ResourceType: resourceType,
		ResourceID: resourceID, EventType: eventType, ReasonCode: reasonCode,
		Payload: []byte(`{"mode":"SIMULATED"}`), OccurredAt: time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC),
	}
}

func assertCashDimensions(t *testing.T, paperService *securities.Service, token, settled, withdrawable, pending, held string) {
	t.Helper()
	account, err := paperService.ResolveSession(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	ledgerService := ledger.NewService(newFinancialTestDatabase(t))
	assertions := []struct {
		dimension ledger.BalanceDimension
		expected  string
	}{
		{ledger.DimensionSettled, settled}, {ledger.DimensionWithdrawable, withdrawable},
		{ledger.DimensionPending, pending}, {ledger.DimensionHeld, held},
	}
	for _, assertion := range assertions {
		actual, err := ledgerService.Balance(t.Context(), account.CashLedgerAccountID, money.MustParseCurrency("USD"), assertion.dimension)
		if err != nil {
			t.Fatal(err)
		}
		if actual.String() != assertion.expected {
			t.Fatalf("expected %s balance %s, got %s", assertion.dimension, assertion.expected, actual.String())
		}
	}
}

func newFinancialTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), requiredEnv(t, "DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
