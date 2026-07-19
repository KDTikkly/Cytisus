//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	cardservice "github.com/KDTikkly/Cytisus/internal/card"
	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/cardapi"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/foundation/migrations"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/notification"
	notificationprovider "github.com/KDTikkly/Cytisus/internal/notification/provider"
	"github.com/KDTikkly/Cytisus/internal/securities"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCardMigrationUpDownUp(t *testing.T) {
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

	if _, err := connection.Exec(ctx, `
		TRUNCATE notification.events, card.customer_profiles,
			compliance.case_events, compliance.cases CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `
		INSERT INTO compliance.cases (
			id, customer_reference, case_type, resource_type, resource_id,
			status, reason_code, next_action, policy_version
		) VALUES (
			'00000000-0000-4000-8000-000000000701', 'paper.card-migration',
			'CARD_DISPUTE', 'card.dispute', 'migration-fixture', 'IN_REVIEW',
			'CARD_TRANSACTION_DISPUTED', 'Review the synthetic fixture.', 'card-sim-v1'
		);
		INSERT INTO compliance.case_events (
			case_id, to_status, actor_type, actor_id, reason_code
		) VALUES (
			'00000000-0000-4000-8000-000000000701', 'IN_REVIEW', 'SYSTEM',
			'card-migration-test', 'CARD_TRANSACTION_DISPUTED'
		)`); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000008_rwa_simulator.down.sql"))
	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000007_card_mvp.down.sql"))
	if _, err := connection.Exec(ctx, `DELETE FROM public.cytisus_schema_migrations WHERE version IN ('000007_card_mvp', '000008_rwa_simulator')`); err != nil {
		t.Fatal(err)
	}
	assertNamedSchemaExists(t, ctx, connection, "card", false)
	assertNamedSchemaExists(t, ctx, connection, "notification", false)
	var cardCaseCount int64
	if err := connection.QueryRow(ctx, "SELECT COUNT(*) FROM compliance.cases WHERE case_type LIKE 'CARD_%'").Scan(&cardCaseCount); err != nil {
		t.Fatal(err)
	}
	if cardCaseCount != 0 {
		t.Fatalf("Card rollback left %d incompatible compliance cases", cardCaseCount)
	}
	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatalf("reapply Card migration: %v", err)
	}
	assertRelationExists(t, ctx, connection, "card.authorizations", true)
	assertRelationExists(t, ctx, connection, "notification.events", true)
	assertRelationExists(t, ctx, connection, "rwa.mint_requests", true)
	var assetCount int64
	if err := connection.QueryRow(ctx, "SELECT COUNT(*) FROM card.collateral_assets").Scan(&assetCount); err != nil {
		t.Fatal(err)
	}
	if assetCount != 4 {
		t.Fatalf("expected four Card collateral fixtures, got %d", assetCount)
	}
}

func TestCardAPIEndToEnd(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService := newPaperService(t, pool, nil, 0)
	registration, err := paperService.RegisterFixture(t.Context(), "card.api-e2e")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 19, 14, 30, 0, 0, time.UTC)
	service := newCardService(t, pool, paperService, func() time.Time { return now })
	mux := http.NewServeMux()
	userHandler := cardapi.NewUser(service, config.EnvironmentTest, "http://localhost:3000")
	mux.Handle("/v1/card", userHandler)
	mux.Handle("/v1/card/", userHandler)
	mux.Handle("/internal/v1/simulators/card/", cardapi.NewSimulator(service, config.EnvironmentTest))
	mux.Handle("/internal/v1/admin/card/", cardapi.NewAdmin(service, config.EnvironmentTest, "http://localhost:3001"))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	unauthorized := apiRequest(t, http.MethodGet, server.URL+"/v1/card", "", "", nil, http.StatusUnauthorized)
	if unauthorized["error"].(map[string]any)["next_action"] == "" {
		t.Fatalf("Card authorization error did not explain the next action: %+v", unauthorized)
	}
	capabilities := apiRequest(t, http.MethodGet, server.URL+"/internal/v1/simulators/card/capabilities", "", "", nil, http.StatusOK)
	if capabilities["mode"] != "SIMULATED" || capabilities["apple_wallet_status"] != "UNAVAILABLE_SIMULATOR" || capabilities["google_wallet_status"] != "UNAVAILABLE_SIMULATOR" {
		t.Fatalf("Card capabilities did not truthfully identify simulator availability: %+v", capabilities)
	}

	created := apiRequest(t, http.MethodPost, server.URL+"/v1/card/cards", registration.AccessToken, "card-create-api-0001", map[string]string{
		"card_type": "VIRTUAL",
	}, http.StatusCreated)
	cardID := created["id"].(string)
	replayedCard := apiRequest(t, http.MethodPost, server.URL+"/v1/card/cards", registration.AccessToken, "card-create-api-0001", map[string]string{
		"card_type": "VIRTUAL",
	}, http.StatusOK)
	if replayedCard["id"] != cardID || replayedCard["replayed"] != true {
		t.Fatalf("Card creation replay changed the resource: %+v", replayedCard)
	}
	duplicateVirtual := apiRequest(t, http.MethodPost, server.URL+"/v1/card/cards", registration.AccessToken, "card-create-api-0002", map[string]string{
		"card_type": "VIRTUAL",
	}, http.StatusConflict)
	if duplicateVirtual["error"].(map[string]any)["code"] != "INVALID_STATE" {
		t.Fatalf("second open virtual Card did not return a stable state error: %+v", duplicateVirtual)
	}
	activated := apiRequest(t, http.MethodPost, server.URL+"/v1/card/cards/"+cardID+"/actions", registration.AccessToken, "card-action-api-0001", map[string]string{
		"action": "ACTIVATE", "reason_code": "USER_ACTIVATED",
	}, http.StatusOK)
	if activated["status"] != "ACTIVE" {
		t.Fatalf("virtual card was not activated: %+v", activated)
	}

	authorizationBody := map[string]any{
		"external_event_id": "card-auth-api-0001", "card_id": cardID,
		"merchant_name": "Synthetic Cafe", "merchant_category_code": "5812",
		"merchant_amount": "100", "merchant_currency": "USD", "entry_mode": "NFC_SIMULATOR",
		"occurred_at": now.Format(time.RFC3339Nano), "simulation_scenario": "NORMAL",
	}
	authorization := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/authorizations", registration.AccessToken, "", authorizationBody, http.StatusOK)
	if authorization["status"] != "APPROVED" || authorization["entry_mode"] != "NFC_SIMULATOR" || authorization["mode"] != "SIMULATED" {
		t.Fatalf("unexpected NFC authorization: %+v", authorization)
	}
	authorizationID := authorization["id"].(string)
	replayedAuthorization := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/authorizations", registration.AccessToken, "", authorizationBody, http.StatusOK)
	if replayedAuthorization["id"] != authorizationID || replayedAuthorization["replayed"] != true {
		t.Fatalf("provider authorization replay was not idempotent: %+v", replayedAuthorization)
	}

	partialCapture := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/captures", registration.AccessToken, "", map[string]any{
		"external_event_id": "card-capture-api-0001", "authorization_id": authorizationID,
		"merchant_amount": "40", "merchant_currency": "USD", "final": false,
		"occurred_at": now.Add(time.Minute).Format(time.RFC3339Nano), "simulation_scenario": "NORMAL",
	}, http.StatusOK)
	if partialCapture["cash_repaid_usd"] == "0" {
		t.Fatalf("partial capture did not settle through Ledger cash repayment: %+v", partialCapture)
	}
	finalCapture := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/captures", registration.AccessToken, "", map[string]any{
		"external_event_id": "card-capture-api-0002", "authorization_id": authorizationID,
		"merchant_amount": "65", "merchant_currency": "USD", "final": true,
		"occurred_at": now.Add(2 * time.Minute).Format(time.RFC3339Nano), "simulation_scenario": "NORMAL",
	}, http.StatusOK)
	if finalCapture["tip_usd"] == "0" {
		t.Fatalf("final partial capture did not represent the incremental tip: %+v", finalCapture)
	}
	captureID := finalCapture["id"].(string)

	refundBody := map[string]any{
		"external_event_id": "card-refund-api-0001", "capture_id": captureID,
		"amount_usd": "10", "occurred_at": now.Add(3 * time.Minute).Format(time.RFC3339Nano),
	}
	refund := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/refunds", registration.AccessToken, "", refundBody, http.StatusOK)
	replayedRefund := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/refunds", registration.AccessToken, "", refundBody, http.StatusOK)
	if refund["ledger_transaction_id"] == "" || replayedRefund["id"] != refund["id"] || replayedRefund["replayed"] != true {
		t.Fatalf("refund did not post and replay idempotently: first=%+v replay=%+v", refund, replayedRefund)
	}

	dispute := apiRequest(t, http.MethodPost, server.URL+"/v1/card/disputes", registration.AccessToken, "card-dispute-api-0001", map[string]string{
		"capture_id": captureID, "amount_usd": "5", "reason_code": "MERCHANDISE_NOT_RECEIVED",
	}, http.StatusCreated)
	resolved := cardAdminRequest(t, http.MethodPost, server.URL+"/internal/v1/admin/card/disputes/"+dispute["id"].(string)+"/resolve", map[string]any{
		"accept": true, "reason_code": "CUSTOMER_EVIDENCE_CONFIRMED",
	}, http.StatusOK)
	if resolved["status"] != "RESOLVED" || resolved["outcome"] != "ACCEPTED" || resolved["ledger_transaction_id"] == nil {
		t.Fatalf("accepted dispute did not produce a Ledger credit: %+v", resolved)
	}

	declined := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/authorizations", registration.AccessToken, "", map[string]any{
		"external_event_id": "card-auth-api-decline", "card_id": cardID,
		"merchant_name": "Synthetic Decline", "merchant_category_code": "5999",
		"merchant_amount": "5", "merchant_currency": "USD", "entry_mode": "ECOMMERCE_SIMULATOR",
		"occurred_at": now.Add(4 * time.Minute).Format(time.RFC3339Nano), "simulation_scenario": "DECLINE",
	}, http.StatusOK)
	if declined["status"] != "DECLINED" || declined["decline_code"] != "SIMULATED_DECLINE" {
		t.Fatalf("decline scenario was not explicit: %+v", declined)
	}
	offline := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/authorizations", registration.AccessToken, "", map[string]any{
		"external_event_id": "card-auth-api-offline", "card_id": cardID,
		"merchant_name": "Synthetic Offline", "merchant_category_code": "5541",
		"merchant_amount": "5", "merchant_currency": "USD", "entry_mode": "NFC_SIMULATOR", "offline": true,
		"occurred_at": now.Add(5 * time.Minute).Format(time.RFC3339Nano), "simulation_scenario": "NORMAL",
	}, http.StatusOK)
	if offline["offline"] != true || offline["entry_mode"] != "OFFLINE_SIMULATOR" {
		t.Fatalf("offline terminal event was not labeled honestly: %+v", offline)
	}
	reversed := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/reversals", registration.AccessToken, "", map[string]any{
		"external_event_id": "card-reversal-api-0001", "authorization_id": offline["id"].(string),
		"amount_usd": "2", "occurred_at": now.Add(6 * time.Minute).Format(time.RFC3339Nano),
	}, http.StatusOK)
	if reversed["reversed_usd"] == "0" {
		t.Fatalf("authorization reversal did not release its Ledger hold: %+v", reversed)
	}

	plastic := apiRequest(t, http.MethodPost, server.URL+"/v1/card/cards", registration.AccessToken, "card-plastic-api-0001", map[string]string{
		"card_type": "PLASTIC",
	}, http.StatusCreated)
	underReview := cardAdminRequest(t, http.MethodPost, server.URL+"/internal/v1/admin/card/cards/"+plastic["id"].(string)+"/lifecycle", map[string]string{
		"next_status": "UNDER_REVIEW", "reason_code": "APPLICATION_RECEIVED",
	}, http.StatusOK)
	if underReview["status"] != "UNDER_REVIEW" {
		t.Fatalf("physical card lifecycle did not advance: %+v", underReview)
	}

	profile := apiRequest(t, http.MethodGet, server.URL+"/v1/card", registration.AccessToken, "", nil, http.StatusOK)
	customerReference := profile["customer_reference"].(string)
	apiRequest(t, http.MethodPost, server.URL+"/v1/card/repayment-mode", registration.AccessToken, "card-mode-api-0001", map[string]string{
		"repayment_mode": "MONTHLY_STATEMENT",
	}, http.StatusOK)
	statementAuthorization := apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/authorizations", registration.AccessToken, "", map[string]any{
		"external_event_id": "card-auth-api-statement", "card_id": cardID,
		"merchant_name": "Synthetic Statement", "merchant_category_code": "5732",
		"merchant_amount": "50", "merchant_currency": "USD", "entry_mode": "ECOMMERCE_SIMULATOR",
		"occurred_at": now.Add(7 * time.Minute).Format(time.RFC3339Nano), "simulation_scenario": "NORMAL",
	}, http.StatusOK)
	apiRequest(t, http.MethodPost, server.URL+"/internal/v1/simulators/card/terminal/captures", registration.AccessToken, "", map[string]any{
		"external_event_id": "card-capture-api-statement", "authorization_id": statementAuthorization["id"].(string),
		"merchant_amount": "50", "merchant_currency": "USD", "final": true,
		"occurred_at": now.Add(8 * time.Minute).Format(time.RFC3339Nano), "simulation_scenario": "NORMAL",
	}, http.StatusOK)
	statement := cardAdminRequest(t, http.MethodPost, server.URL+"/internal/v1/admin/card/statements", map[string]string{
		"customer_reference": customerReference, "period_start": "2026-07-01", "period_end": "2026-07-19",
	}, http.StatusCreated)
	if statement["status"] != "OPEN" || statement["amount_due_usd"] == "0" {
		t.Fatalf("monthly statement did not expose the receivable: %+v", statement)
	}
	paid := apiRequest(t, http.MethodPost, server.URL+"/v1/card/statements/"+statement["id"].(string)+"/pay", registration.AccessToken, "card-statement-pay-0001", nil, http.StatusOK)
	if paid["status"] != "PAID" || paid["repayment_ledger_transaction_id"] == nil {
		t.Fatalf("statement payment did not post through Ledger: %+v", paid)
	}

	reconciliation := cardAdminRequest(t, http.MethodPost, server.URL+"/internal/v1/admin/card/reconciliation", map[string]any{
		"customer_reference": customerReference,
	}, http.StatusOK)
	if reconciliation["status"] != "COMPLETED_WITHOUT_DIFFERENCE" || reconciliation["difference_usd"] != "0" {
		t.Fatalf("Card reconciliation should not auto-adjust or invent a difference: %+v", reconciliation)
	}
	notifications := apiRequest(t, http.MethodGet, server.URL+"/v1/card/notifications", registration.AccessToken, "", nil, http.StatusOK)
	if len(notifications["items"].([]any)) < 10 {
		t.Fatalf("expected user-visible Card notification history: %+v", notifications)
	}

	assertCountAtLeast(t, pool, 1, "SELECT COUNT(*) FROM audit.events WHERE action LIKE 'card.%'")
	assertCountAtLeast(t, pool, 1, "SELECT COUNT(*) FROM outbox.events WHERE event_type LIKE 'card.%' OR event_type = 'notification.event.created'")
	assertLedgerBalanced(t, pool)
}

func TestCardProtectedAutoSellAndConcurrentReplay(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService := newPaperService(t, pool, nil, 0)
	registration, err := paperService.RegisterFixture(t.Context(), "card.auto-sell")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 19, 15, 0, 0, 0, time.UTC)
	service := newCardService(t, pool, paperService, func() time.Time { return now })
	created, err := service.CreateCard(t.Context(), cardservice.CreateCardCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "card-auto-create-0001", Type: cardservice.TypeVirtual,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ActOnCard(t.Context(), cardservice.CardActionCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "card-auto-activate-0001", CardID: created.ID, Action: "ACTIVATE", ReasonCode: "USER_ACTIVATED",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfigureRepayment(t.Context(), cardservice.ConfigureRepaymentCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "card-auto-mode-0001", Mode: cardservice.RepaymentCashThenAutoSell,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateCollateralQuote(t.Context(), cardservice.UpdateCollateralQuoteCommand{
		Symbol: "TLT", ReferencePrice: money.MustParse("90"), QuoteStatus: "SIMULATED", MarketStatus: "OPEN", ObservedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SeedCollateral(t.Context(), cardservice.SeedCollateralCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "card-auto-seed-0001", Symbol: "TLT", Quantity: money.MustParse("1000"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfigureAutoSellMandate(t.Context(), cardservice.ConfigureMandateCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "card-auto-mandate-0001",
		Enabled: true, AllowFractional: true, DailyMaxUSD: money.MustParse("25000"), ValidUntil: now.Add(30 * 24 * time.Hour),
		Assets: []cardservice.AutoSellMandateAsset{{Priority: 1, Symbol: "TLT", MinimumRetainQuantity: money.Zero()}},
	}); err != nil {
		t.Fatal(err)
	}
	power, err := service.SpendingPower(t.Context(), registration.AccessToken)
	if err != nil || !power.CollateralEligibleUSD.IsPositive() || power.RepaymentMode != cardservice.RepaymentCashThenAutoSell {
		t.Fatalf("dynamic asset-backed spending power was not available: %+v %v", power, err)
	}

	authorization, err := service.Authorize(t.Context(), cardservice.AuthorizeCommand{
		AccessToken: registration.AccessToken, ExternalEventID: "card-auto-auth-0001", CardID: created.ID,
		MerchantName: "Synthetic Equipment", MerchantCategoryCode: "5045", MerchantAmount: money.MustParse("110000"),
		MerchantCurrency: "USD", EntryMode: "NFC_SIMULATOR", OccurredAt: now, SimulationScenario: cardprovider.ScenarioNormal,
	})
	if err != nil || authorization.Status != "APPROVED" {
		t.Fatalf("asset-backed authorization failed: %+v %v", authorization, err)
	}
	capture, err := service.Capture(t.Context(), cardservice.CaptureCommand{
		AccessToken: registration.AccessToken, ExternalEventID: "card-auto-capture-0001", AuthorizationID: authorization.ID,
		MerchantAmount: money.MustParse("110000"), MerchantCurrency: "USD", Final: true,
		OccurredAt: now.Add(time.Minute), SimulationScenario: cardprovider.ScenarioNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !capture.AutoSellRepaidUSD.IsPositive() || len(capture.AutoSellExecutions) != 1 || capture.AutoSellExecutions[0].Status != "FILLED" ||
		capture.AutoSellExecutions[0].ExecutionPrice.Compare(capture.AutoSellExecutions[0].ProtectedLimitPrice) < 0 {
		t.Fatalf("capture did not use a protected, fully covered Auto-Sell: %+v", capture)
	}
	profile, err := service.Profile(t.Context(), registration.AccessToken)
	if err != nil || profile.SpendingStatus != "ACTIVE" {
		t.Fatalf("successful protected Auto-Sell incorrectly froze Card spending: %+v %v", profile, err)
	}

	concurrentCommand := cardservice.AuthorizeCommand{
		AccessToken: registration.AccessToken, ExternalEventID: "card-auto-auth-concurrent", CardID: created.ID,
		MerchantName: "Synthetic Concurrent", MerchantCategoryCode: "5999", MerchantAmount: money.MustParse("100"),
		MerchantCurrency: "USD", EntryMode: "ECOMMERCE_SIMULATOR", OccurredAt: now.Add(2 * time.Minute), SimulationScenario: cardprovider.ScenarioNormal,
	}
	const goroutines = 8
	results := make(chan cardservice.Authorization, goroutines)
	errorsChannel := make(chan error, goroutines)
	var group sync.WaitGroup
	for range goroutines {
		group.Add(1)
		go func() {
			defer group.Done()
			result, authorizeErr := service.Authorize(context.Background(), concurrentCommand)
			if authorizeErr != nil {
				errorsChannel <- authorizeErr
				return
			}
			results <- result
		}()
	}
	group.Wait()
	close(results)
	close(errorsChannel)
	for concurrentErr := range errorsChannel {
		t.Fatalf("concurrent Card provider replay failed: %v", concurrentErr)
	}
	identifier := ""
	replayCount := 0
	for result := range results {
		if identifier == "" {
			identifier = result.ID
		}
		if result.ID != identifier {
			t.Fatalf("concurrent provider replay created multiple authorizations: %s and %s", identifier, result.ID)
		}
		if result.Replayed {
			replayCount++
		}
	}
	if replayCount != goroutines-1 {
		t.Fatalf("expected %d concurrent replays, got %d", goroutines-1, replayCount)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM card.provider_events WHERE external_event_id = $1", concurrentCommand.ExternalEventID)
	assertLedgerBalanced(t, pool)
}

type cardSessionResolver struct {
	service *securities.Service
}

func (resolver cardSessionResolver) ResolveSession(ctx context.Context, accessToken string) (cardservice.CustomerSession, error) {
	account, err := resolver.service.ResolveSession(ctx, accessToken)
	if err != nil {
		return cardservice.CustomerSession{}, err
	}
	return cardservice.CustomerSession{
		PaperAccountID: account.ID, CustomerReference: account.CustomerReference, CashLedgerAccountID: account.CashLedgerAccountID,
	}, nil
}

func newCardService(t *testing.T, pool *pgxpool.Pool, paperService *securities.Service, now func() time.Time) *cardservice.Service {
	t.Helper()
	provider, err := cardprovider.NewLocal(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	provider.WithClock(now)
	notificationAdapter, err := notificationprovider.NewLocal(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	notifications, err := notification.NewService(notificationAdapter)
	if err != nil {
		t.Fatal(err)
	}
	service, err := cardservice.NewService(cardservice.Dependencies{
		Database: pool, Environment: config.EnvironmentTest, Resolver: cardSessionResolver{service: paperService},
		Provider: provider, Notifications: notifications, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func cardAdminRequest(t *testing.T, method, target string, body any, expectedStatus int) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Admin-ID", "synthetic-operations-001")
	request.Header.Set("X-Admin-Role", "OPERATIONS")
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

func assertCountAtLeast(t *testing.T, pool *pgxpool.Pool, expected int64, query string, arguments ...any) {
	t.Helper()
	var actual int64
	if err := pool.QueryRow(t.Context(), query, arguments...).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual < expected {
		t.Fatalf("expected count at least %d, got %d for %s", expected, actual, query)
	}
}

func assertLedgerBalanced(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	assertCount(t, pool, 0, `
		SELECT COUNT(*)
		FROM (
			SELECT transaction_id, currency, balance_dimension
			FROM ledger.entries
			GROUP BY transaction_id, currency, balance_dimension
			HAVING SUM(CASE direction WHEN 'DEBIT' THEN amount ELSE -amount END) <> 0
		) unbalanced`)
}
