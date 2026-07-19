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
	cryptoservice "github.com/KDTikkly/Cytisus/internal/crypto"
	cryptoprovider "github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/cryptoapi"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/foundation/migrations"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCryptoMigrationUpDownUp(t *testing.T) {
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

	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000006_crypto_routing.down.sql"))
	if _, err := connection.Exec(ctx, "DELETE FROM public.cytisus_schema_migrations WHERE version = $1", "000006_crypto_routing"); err != nil {
		t.Fatal(err)
	}
	assertNamedSchemaExists(t, ctx, connection, "crypto", false)
	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatalf("reapply crypto migration: %v", err)
	}
	assertRelationExists(t, ctx, connection, "crypto.conversions", true)
	assertRelationExists(t, ctx, connection, "crypto.withdrawal_addresses", true)
	var assetCount, venueCount int64
	if err := connection.QueryRow(ctx, "SELECT COUNT(*) FROM crypto.assets").Scan(&assetCount); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRow(ctx, "SELECT COUNT(*) FROM crypto.venues").Scan(&venueCount); err != nil {
		t.Fatal(err)
	}
	if assetCount != 12 || venueCount != 3 {
		t.Fatalf("expected twelve assets and three venues, got assets=%d venues=%d", assetCount, venueCount)
	}
}

func TestCryptoAPIEndToEnd(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService := newPaperService(t, pool, nil, 0)
	registration, err := paperService.RegisterFixture(t.Context(), "crypto.api-e2e")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service := newCryptoService(t, pool, paperService, func() time.Time { return now })
	mux := http.NewServeMux()
	mux.Handle("/v1/crypto/", cryptoapi.NewUser(service, config.EnvironmentTest, ""))
	mux.Handle("/internal/v1/simulators/crypto/", cryptoapi.NewSimulator(service, config.EnvironmentTest))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	assets := apiRequest(t, http.MethodGet, server.URL+"/v1/crypto/assets", "", "", nil, http.StatusOK)
	if len(assets["items"].([]any)) != 12 || assets["quote_currency"] != "USD" || assets["mode"] != "SIMULATED" {
		t.Fatalf("unexpected crypto asset contract: %+v", assets)
	}
	unauthorized := apiRequest(t, http.MethodGet, server.URL+"/v1/crypto/portfolio", "", "", nil, http.StatusUnauthorized)
	if unauthorized["error"].(map[string]any)["next_action"] == "" {
		t.Fatalf("crypto authorization error did not explain next action: %+v", unauthorized)
	}
	conversion := apiRequest(t, http.MethodPost, server.URL+"/v1/crypto/conversions", registration.AccessToken, "crypto-api-convert-0001", map[string]any{
		"source_asset": "USD", "destination_asset": "BTC", "source_amount": "500", "simulation_scenario": "NORMAL",
	}, http.StatusCreated)
	legs := conversion["legs"].([]any)
	if conversion["status"] != "FILLED" || len(legs) != 1 || legs[0].(map[string]any)["quote_currency"] != "USD" || conversion["mode"] != "SIMULATED" {
		t.Fatalf("unexpected crypto conversion contract: %+v", conversion)
	}
}

func TestCryptoRoutingCustodyReviewAndReconciliation(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService := newPaperService(t, pool, nil, 0)
	registration, err := paperService.RegisterFixture(t.Context(), "crypto.vertical-slice")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service := newCryptoService(t, pool, paperService, func() time.Time { return now })

	assets, err := service.ListAssets(t.Context())
	if err != nil || len(assets) != 12 {
		t.Fatalf("unexpected crypto assets: count=%d err=%v", len(assets), err)
	}
	stablecoins := 0
	for _, asset := range assets {
		if asset.IsStablecoin {
			stablecoins++
		}
		for _, network := range asset.Networks {
			if !network.NativeDeployment {
				t.Fatalf("non-native hidden bridge exposed: %+v", network)
			}
		}
	}
	if stablecoins != 2 {
		t.Fatalf("expected two market-priced stablecoins, got %d", stablecoins)
	}

	depositAddress, err := service.CreateDepositAddress(t.Context(), cryptoservice.CreateDepositAddressCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("deposit-address-0001"), Asset: "BTC", Network: "BITCOIN",
	})
	if err != nil || depositAddress.Status != "ACTIVE" || depositAddress.Provider != "local-custody-simulator" {
		t.Fatalf("unexpected deposit address: %+v %v", depositAddress, err)
	}
	depositEvent := cryptoservice.DepositEvent{
		Provider: "local-custody-simulator", ExternalEventID: "crypto-deposit-confirm-0001",
		DepositAddressID: depositAddress.ID, Asset: "BTC", Network: "BITCOIN", Quantity: money.MustParse("1"),
		EventType: "DEPOSIT_CONFIRMED", ProviderTransactionID: "simulated-chain-btc-0001",
		OccurredAt: now, Payload: []byte(`{"mode":"SIMULATED","confirmations":"6"}`),
	}
	deposit, err := service.ApplyDepositEvent(t.Context(), depositEvent)
	if err != nil || deposit.Status != "CONFIRMED" || deposit.LedgerTransactionID == "" {
		t.Fatalf("unexpected confirmed crypto deposit: %+v %v", deposit, err)
	}
	replayedDeposit, err := service.ApplyDepositEvent(t.Context(), depositEvent)
	if err != nil || !replayedDeposit.Replayed || replayedDeposit.ID != deposit.ID {
		t.Fatalf("expected deposit callback replay: %+v %v", replayedDeposit, err)
	}
	changedDeposit := depositEvent
	changedDeposit.Payload = []byte(`{"mode":"SIMULATED","confirmations":"7"}`)
	if _, err := service.ApplyDepositEvent(t.Context(), changedDeposit); !errors.Is(err, cryptoservice.ErrProviderEventConflict) {
		t.Fatalf("expected deposit event conflict, got %v", err)
	}

	usdBuy := convertCrypto(t, service, cryptoservice.ConvertCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("usd-buy-0001"),
		SourceAsset: "USD", DestinationAsset: "BTC", SourceAmount: money.MustParse("1000"), SimulationScenario: cryptoprovider.ScenarioNormal,
	})
	assertCryptoLegs(t, usdBuy, 1)
	if usdBuy.Legs[0].Side != "BUY" || !usdBuy.Legs[0].VenueFeeUSD.IsPositive() || !usdBuy.Legs[0].PlatformFeeUSD.IsPositive() {
		t.Fatalf("buy did not expose explicit fees: %+v", usdBuy.Legs[0])
	}
	replayedBuy, err := service.Convert(t.Context(), cryptoservice.ConvertCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("usd-buy-0001"),
		SourceAsset: "USD", DestinationAsset: "BTC", SourceAmount: money.MustParse("1000"), SimulationScenario: cryptoprovider.ScenarioNormal,
	})
	if err != nil || !replayedBuy.Replayed || replayedBuy.ID != usdBuy.ID {
		t.Fatalf("expected conversion replay: %+v %v", replayedBuy, err)
	}

	cross := convertCrypto(t, service, cryptoservice.ConvertCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("cross-asset-0001"),
		SourceAsset: "BTC", DestinationAsset: "ETH", SourceAmount: money.MustParse("0.2"), SimulationScenario: cryptoprovider.ScenarioNormal,
	})
	assertCryptoLegs(t, cross, 2)
	if cross.Legs[0].Side != "SELL" || cross.Legs[1].Side != "BUY" || cross.Legs[0].QuoteCurrency != "USD" || cross.Legs[1].QuoteCurrency != "USD" {
		t.Fatalf("cross-asset conversion did not use two explicit USD legs: %+v", cross.Legs)
	}

	partial := convertCrypto(t, service, cryptoservice.ConvertCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("partial-sell-0001"),
		SourceAsset: "BTC", DestinationAsset: "USD", SourceAmount: money.MustParse("0.5"), SimulationScenario: cryptoprovider.ScenarioPartial,
	})
	if partial.Status != "PARTIALLY_FILLED" || len(partial.Legs[0].Children) < 2 || partial.Legs[0].FilledQuantity.Compare(partial.SourceAmount) >= 0 {
		t.Fatalf("expected visible split partial fill: %+v", partial)
	}

	depeg := convertCrypto(t, service, cryptoservice.ConvertCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("usdc-depeg-0001"),
		SourceAsset: "USD", DestinationAsset: "USDC", SourceAmount: money.MustParse("100"), SimulationScenario: cryptoprovider.ScenarioStablecoinDepeg,
	})
	if depeg.Legs[0].AverageExecutionPrice.Compare(money.MustParse("0.99")) >= 0 {
		t.Fatalf("stablecoin was fixed to one USD: %+v", depeg.Legs[0])
	}

	review := convertCrypto(t, service, cryptoservice.ConvertCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("review-cross-0001"),
		SourceAsset: "BTC", DestinationAsset: "SOL", SourceAmount: money.MustParse("0.01"),
		SimulationScenario: cryptoprovider.ScenarioNormal, SimulationRiskFlag: true,
	})
	if review.Status != "REVIEW_REQUIRED" || review.ComplianceCaseID == "" || len(review.Legs) != 2 || review.Legs[1].Status != "BLOCKED" {
		t.Fatalf("crypto-to-USD review did not freeze and block the second leg: %+v", review)
	}
	assertPositiveLedgerBalance(t, pool, registration.Account.CashLedgerAccountID, "USD", ledger.DimensionFrozen)

	address, err := service.AddWithdrawalAddress(t.Context(), cryptoservice.AddWithdrawalAddressCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("withdraw-address-0001"),
		Asset: "BTC", Network: "BITCOIN", ExternalAddress: "simulated-external-btc-0001", Label: "Primary simulated wallet",
		RiskContext: cryptoservice.AddressRiskContext{UntrustedDevice: true, RecentSecurityChange: true, RecentRecovery: true},
	})
	if err != nil || address.Status != "COOLING_OFF" || address.CoolingUntil == nil {
		t.Fatalf("unexpected withdrawal address cooling: %+v %v", address, err)
	}
	expectedCooling := now.Add(72 * time.Hour)
	if !address.CoolingUntil.Equal(expectedCooling.Truncate(time.Microsecond)) {
		t.Fatalf("expected capped 72h cooling until %s, got %s", expectedCooling, address.CoolingUntil)
	}
	if _, err := service.ActivateWithdrawalAddress(t.Context(), cryptoservice.ActivateWithdrawalAddressCommand{
		AccessToken: registration.AccessToken, AddressID: address.ID,
	}); !errors.Is(err, cryptoservice.ErrCoolingOff) {
		t.Fatalf("expected cooling to block activation, got %v", err)
	}
	now = expectedCooling.Add(time.Second)
	address, err = service.ActivateWithdrawalAddress(t.Context(), cryptoservice.ActivateWithdrawalAddressCommand{
		AccessToken: registration.AccessToken, AddressID: address.ID,
	})
	if err != nil || address.Status != "ACTIVE" {
		t.Fatalf("unexpected activated withdrawal address: %+v %v", address, err)
	}

	withdrawal, err := service.RequestWithdrawal(t.Context(), cryptoservice.RequestWithdrawalCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("withdrawal-request-0001"),
		AddressID: address.ID, Quantity: money.MustParse("0.05"),
	})
	if err != nil || withdrawal.Status != "APPROVED" {
		t.Fatalf("unexpected crypto withdrawal approval: %+v %v", withdrawal, err)
	}
	broadcastEvent := cryptoWithdrawalEvent(now, "crypto-withdraw-broadcast-0001", withdrawal.ID, "WITHDRAWAL_BROADCAST", "")
	withdrawal, err = service.ApplyWithdrawalEvent(t.Context(), broadcastEvent)
	if err != nil || withdrawal.Status != "BROADCAST" {
		t.Fatalf("unexpected crypto broadcast: %+v %v", withdrawal, err)
	}
	replayedWithdrawal, err := service.ApplyWithdrawalEvent(t.Context(), broadcastEvent)
	if err != nil || !replayedWithdrawal.Replayed {
		t.Fatalf("expected withdrawal callback replay: %+v %v", replayedWithdrawal, err)
	}
	withdrawal, err = service.ApplyWithdrawalEvent(t.Context(), cryptoWithdrawalEvent(now, "crypto-withdraw-confirm-0001", withdrawal.ID, "WITHDRAWAL_CONFIRMED", ""))
	if err != nil || withdrawal.Status != "CONFIRMED" {
		t.Fatalf("unexpected crypto confirmation: %+v %v", withdrawal, err)
	}

	failed, err := service.RequestWithdrawal(t.Context(), cryptoservice.RequestWithdrawalCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("withdrawal-request-0002"),
		AddressID: address.ID, Quantity: money.MustParse("0.01"),
	})
	if err != nil {
		t.Fatal(err)
	}
	failed, err = service.ApplyWithdrawalEvent(t.Context(), cryptoWithdrawalEvent(now, "crypto-withdraw-failed-0001", failed.ID, "WITHDRAWAL_FAILED", "NETWORK_REJECTED"))
	if err != nil || failed.Status != "FAILED" {
		t.Fatalf("failed withdrawal did not restore reservation: %+v %v", failed, err)
	}

	matched, err := service.Reconcile(t.Context(), cryptoservice.ReconcileCommand{
		Actor:             cryptoservice.AdminActor{ID: "ops.fixture", Role: "OPERATIONS"},
		CustomerReference: registration.Account.CustomerReference, Asset: "BTC",
	})
	if err != nil || matched.Status != "MATCHED" || !matched.Difference.IsZero() {
		t.Fatalf("unexpected matched crypto reconciliation: %+v %v", matched, err)
	}
	custodyDifference := money.Zero()
	difference, err := service.Reconcile(t.Context(), cryptoservice.ReconcileCommand{
		Actor:             cryptoservice.AdminActor{ID: "risk.fixture", Role: "RISK_ANALYST"},
		CustomerReference: registration.Account.CustomerReference, Asset: "BTC", CustodyAmount: &custodyDifference,
	})
	if !errors.Is(err, cryptoservice.ErrReconciliationDifference) || difference.Status != "DIFFERENCE" || difference.ComplianceCase == "" {
		t.Fatalf("expected visible custody reconciliation difference: %+v %v", difference, err)
	}

	assertCount(t, pool, 0, `
		SELECT COUNT(*) FROM (
			SELECT transaction_id, currency
			FROM ledger.entries
			GROUP BY transaction_id, currency
			HAVING SUM(amount) FILTER (WHERE direction = 'DEBIT') <> SUM(amount) FILTER (WHERE direction = 'CREDIT')
		) AS unbalanced`)
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM audit.events WHERE action LIKE 'crypto.%' AND actor_id = ''")
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM outbox.events WHERE aggregate_type LIKE 'crypto.%' AND payload = '{}'::JSONB")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM compliance.cases WHERE id = $1 AND reason_code = 'CRYPTO_TO_USD_REVIEW'", review.ComplianceCaseID)
	_, err = pool.Exec(t.Context(), "UPDATE crypto.legs SET gross_usd = gross_usd + 1 WHERE conversion_id = $1", cross.ID)
	assertSQLState(t, err, "55000")
}

func TestCryptoConcurrentIdempotencyAndUnsettledCashIsolation(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService := newPaperService(t, pool, nil, 0)
	registration, err := paperService.RegisterFixture(t.Context(), "crypto.concurrent")
	if err != nil {
		t.Fatal(err)
	}
	service := newCryptoService(t, pool, paperService, time.Now)
	command := cryptoservice.ConvertCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("concurrent-convert-0001"),
		SourceAsset: "USD", DestinationAsset: "DOGE", SourceAmount: money.MustParse("50"), SimulationScenario: cryptoprovider.ScenarioNormal,
	}
	const callers = 6
	results := make(chan cryptoservice.Conversion, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, convertErr := service.Convert(t.Context(), command)
			if convertErr != nil {
				errorsFound <- convertErr
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for found := range errorsFound {
		t.Errorf("concurrent crypto conversion failed: %v", found)
	}
	identifier := ""
	for result := range results {
		if identifier == "" {
			identifier = result.ID
		}
		if result.ID != identifier {
			t.Fatalf("expected one crypto conversion, got %s and %s", identifier, result.ID)
		}
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM crypto.conversions")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM crypto.legs")

	bankService := newBankingService(t, pool, paperService, nil)
	account, err := bankService.LinkBankAccount(t.Context(), banking.LinkBankAccountCommand{
		AccessToken: registration.AccessToken, ExternalAccountReference: "fixture-crypto-unsettled-ach",
		RailSupport: "ACH", OwnerRelation: "SAME_NAME", RiskClass: "STANDARD",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, _, err = bankService.VerifyBankAccountOwnership(t.Context(), bankEvent("crypto-ach-ownership-0001", "BANK_ACCOUNT", account.ID, "OWNERSHIP_VERIFIED", "OWNERSHIP_CONFIRMED"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bankService.InitiateFunding(t.Context(), banking.InitiateFundingCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("ach-pending-0001"),
		BankAccountID: account.ID, Rail: "ACH", Amount: money.MustParse("1000"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Convert(t.Context(), cryptoservice.ConvertCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: cryptoRequestID("provisional-buy-0001"),
		SourceAsset: "USD", DestinationAsset: "ETH", SourceAmount: money.MustParse("100500"), SimulationScenario: cryptoprovider.ScenarioNormal,
	}); !errors.Is(err, cryptoservice.ErrInsufficientCash) {
		t.Fatalf("unsettled ACH increased crypto buying cash: %v", err)
	}
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM crypto.conversion_requests WHERE idempotency_key = 'crypto-provisional-buy-0001'")
}

type cryptoSessionResolver struct {
	service *securities.Service
}

func (resolver cryptoSessionResolver) ResolveSession(ctx context.Context, token string) (cryptoservice.CustomerSession, error) {
	account, err := resolver.service.ResolveSession(ctx, token)
	if err != nil {
		return cryptoservice.CustomerSession{}, err
	}
	return cryptoservice.CustomerSession{
		PaperAccountID: account.ID, CustomerReference: account.CustomerReference, CashLedgerAccountID: account.CashLedgerAccountID,
	}, nil
}

func newCryptoService(t *testing.T, pool *pgxpool.Pool, paperService *securities.Service, now func() time.Time) *cryptoservice.Service {
	t.Helper()
	venues := make([]cryptoprovider.VenueAdapter, 0, 3)
	for _, code := range []string{"VENUE_A", "VENUE_B", "VENUE_C"} {
		venue, err := cryptoprovider.NewLocalVenue(config.EnvironmentTest, code)
		if err != nil {
			t.Fatal(err)
		}
		venues = append(venues, venue)
	}
	custody, err := cryptoprovider.NewLocalCustody(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	chainAnalytics, err := cryptoprovider.NewLocalChainAnalytics(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	service, err := cryptoservice.NewService(cryptoservice.Dependencies{
		Database: pool, Environment: config.EnvironmentTest, Resolver: cryptoSessionResolver{service: paperService},
		Venues: venues, Custody: custody, ChainAnalytics: chainAnalytics, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func convertCrypto(t *testing.T, service *cryptoservice.Service, command cryptoservice.ConvertCommand) cryptoservice.Conversion {
	t.Helper()
	converted, err := service.Convert(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	return converted
}

func cryptoRequestID(suffix string) string {
	return "crypto-" + suffix
}

func assertCryptoLegs(t *testing.T, conversion cryptoservice.Conversion, expected int) {
	t.Helper()
	if conversion.Status != "FILLED" || len(conversion.Legs) != expected {
		t.Fatalf("unexpected crypto conversion: %+v", conversion)
	}
	for _, leg := range conversion.Legs {
		if leg.QuoteCurrency != "USD" || leg.LedgerTransactionID == "" || len(leg.Children) == 0 {
			t.Fatalf("crypto leg missed USD, Ledger, or venue evidence: %+v", leg)
		}
	}
}

func cryptoWithdrawalEvent(now time.Time, externalID, withdrawalID, eventType, reasonCode string) cryptoservice.WithdrawalEvent {
	return cryptoservice.WithdrawalEvent{
		Provider: "local-custody-simulator", ExternalEventID: externalID, WithdrawalID: withdrawalID,
		EventType: eventType, ReasonCode: reasonCode, OccurredAt: now, Payload: []byte(`{"mode":"SIMULATED"}`),
	}
}

func assertPositiveLedgerBalance(t *testing.T, pool *pgxpool.Pool, accountID, currency string, dimension ledger.BalanceDimension) {
	t.Helper()
	balance, err := ledger.NewService(pool).Balance(t.Context(), accountID, money.Currency(currency), dimension)
	if err != nil {
		t.Fatal(err)
	}
	if !balance.IsPositive() {
		t.Fatalf("expected positive %s %s balance, got %s", currency, dimension, balance.String())
	}
}
