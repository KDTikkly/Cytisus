//go:build integration

package integration

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/foundation/migrations"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	rwaservice "github.com/KDTikkly/Cytisus/internal/rwa"
	rwaprovider "github.com/KDTikkly/Cytisus/internal/rwa/provider"
	"github.com/KDTikkly/Cytisus/internal/securities"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const rwaAssetID = "61c443ab-0d51-4cf2-9133-29308738f741"

func TestRwaMigrationUpDownUp(t *testing.T) {
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
	applyMigrationFile(t, ctx, connection, filepath.Join(directory, "000008_rwa_simulator.down.sql"))
	if _, err := connection.Exec(ctx, "DELETE FROM public.cytisus_schema_migrations WHERE version = '000008_rwa_simulator'"); err != nil {
		t.Fatal(err)
	}
	assertNamedSchemaExists(t, ctx, connection, "rwa", false)
	assertRelationExists(t, ctx, connection, "securities.position_reservations", false)
	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatalf("reapply RWA migration: %v", err)
	}
	assertRelationExists(t, ctx, connection, "rwa.mint_requests", true)
	assertRelationExists(t, ctx, connection, "rwa.redemption_requests", true)
	assertRelationExists(t, ctx, connection, "securities.position_reservations", true)
	assertCount(t, mustPool(t, databaseURL), 1, "SELECT COUNT(*) FROM rwa.assets WHERE token_decimals = 0 AND underlying_symbol = 'AAPL'")
}

func TestRwaMintDividendBurnRecoveryAndInvariants(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService, registration := paperPositionFixture(t, pool, "rwa.vertical", "4")
	chain := newFakeRwaProvider()
	custodian := &failingShareCustodian{service: paperService}
	service := newRwaService(t, pool, paperService, custodian, chain)

	minted, err := service.Mint(t.Context(), rwaservice.MintCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "rwa-rwa-rwa-0001",
		AssetID: rwaAssetID, Quantity: money.MustParse("2"), CustodyMode: rwaservice.CustodyVault,
	})
	if err != nil || minted.Status != "MINTED" || minted.Quantity.String() != "2" {
		t.Fatalf("mint=%+v err=%v", minted, err)
	}
	replayed, err := service.Mint(t.Context(), rwaservice.MintCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "rwa-rwa-rwa-0001",
		AssetID: rwaAssetID, Quantity: money.MustParse("2"), CustodyMode: rwaservice.CustodyVault,
	})
	if err != nil || replayed.ID != minted.ID {
		t.Fatalf("mint replay=%+v err=%v", replayed, err)
	}
	if _, err := service.Mint(t.Context(), rwaservice.MintCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "rwa-mint-fractional-0001",
		AssetID: rwaAssetID, Quantity: money.MustParse("0.5"), CustodyMode: rwaservice.CustodyVault,
	}); !errors.Is(err, rwaservice.ErrInvalidCommand) {
		t.Fatalf("expected fractional rejection, got %v", err)
	}
	if _, err := paperService.SubmitOrder(t.Context(), securities.SubmitOrderCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "rwa-locked-sell-0001", Symbol: "AAPL",
		Side: broker.SideSell, OrderType: broker.OrderTypeMarket, TimeInForce: broker.TimeInForceDay,
		Quantity: money.MustParse("3"),
	}); !errors.Is(err, securities.ErrInsufficientPosition) {
		t.Fatalf("locked shares were sellable: %v", err)
	}
	holdings, err := service.Portfolio(t.Context(), registration.AccessToken)
	if err != nil || len(holdings) != 1 || holdings[0].Quantity.String() != "2" {
		t.Fatalf("holdings=%+v err=%v", holdings, err)
	}

	account, _ := paperService.ResolveSession(t.Context(), registration.AccessToken)
	beforeCash, err := ledger.NewService(pool).Balance(t.Context(), account.CashLedgerAccountID, money.Currency("USD"), ledger.DimensionSettled)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 19, 16, 0, 0, 0, time.UTC)
	dividend, err := service.AnnounceDividend(t.Context(), rwaservice.AnnounceDividendCommand{
		AssetID: rwaAssetID, ExternalReference: "rwa-dividend-fixture-0001", USDPerShare: money.MustParse("1.25"),
		RecordAt: now.Add(-time.Hour), PayableAt: now.Add(-time.Minute), Actor: rwaservice.AdminActor{ID: "ops-dividend", Role: rwaservice.AdminRoleOps},
	})
	if err != nil {
		t.Fatal(err)
	}
	processed, entitlements, err := service.ProcessDividend(t.Context(), rwaservice.ProcessDividendCommand{DividendID: dividend.ID, Actor: rwaservice.AdminActor{ID: "ops-dividend", Role: rwaservice.AdminRoleOps}})
	if err != nil || processed.Status != "RECONCILED" || len(entitlements) != 1 || entitlements[0].NetUSD.String() != "2.5" {
		t.Fatalf("dividend=%+v entitlements=%+v err=%v", processed, entitlements, err)
	}
	afterCash, _ := ledger.NewService(pool).Balance(t.Context(), account.CashLedgerAccountID, money.Currency("USD"), ledger.DimensionSettled)
	delta, _ := afterCash.Sub(beforeCash)
	if delta.String() != "2.5" {
		t.Fatalf("dividend did not credit Settled USD Cash: %s", delta)
	}

	custodian.failNextRelease = true
	_, err = service.Redeem(t.Context(), rwaservice.RedeemCommand{AccessToken: registration.AccessToken, IdempotencyKey: "rwa-rwa-rwa-0002", MintID: minted.ID})
	if err == nil {
		t.Fatal("expected simulated release failure after confirmed burn")
	}
	redemptions, err := service.ListRedemptions(t.Context(), registration.AccessToken)
	if err != nil || len(redemptions) != 1 || redemptions[0].Status != "CHAIN_PENDING" {
		t.Fatalf("recoverable redemption=%+v err=%v", redemptions, err)
	}
	if err := service.RecoverOperation(t.Context(), rwaservice.RecoverCommand{OperationID: redemptions[0].OperationID, Actor: rwaservice.AdminActor{ID: "ops-recovery", Role: rwaservice.AdminRoleOps}}); err != nil {
		t.Fatal(err)
	}
	redemptions, _ = service.ListRedemptions(t.Context(), registration.AccessToken)
	if redemptions[0].Status != "COMPLETED" {
		t.Fatalf("redemption was not recovered: %+v", redemptions[0])
	}

	completed, err := service.RunDailyReconciliation(t.Context())
	if err != nil || completed != 1 {
		t.Fatalf("daily reconciliation completed=%d err=%v", completed, err)
	}
	completed, err = service.RunDailyReconciliation(t.Context())
	if err != nil || completed != 0 {
		t.Fatalf("same-day reconciliation replay completed=%d err=%v", completed, err)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM rwa.reconciliation_runs WHERE status = 'BALANCED' AND difference = 0")
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM securities.position_reservations WHERE status = 'ACTIVE'")
	assertCount(t, pool, 0, `SELECT COUNT(*) FROM (
		SELECT transaction_id, currency FROM ledger.entries GROUP BY transaction_id, currency
		HAVING SUM(amount) FILTER (WHERE direction = 'DEBIT') <> SUM(amount) FILTER (WHERE direction = 'CREDIT')
	) AS unbalanced`)
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM rwa.dividends WHERE status = 'RECONCILED' AND id NOT IN (SELECT dividend_id FROM rwa.dividend_entitlements WHERE status = 'USD_CASH_CREDITED')")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM audit.events WHERE action = 'rwa.redemption.completed'")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM outbox.events WHERE event_type = 'rwa.redemption.completed'")
}

func TestRwaMintChainFailureReleasesOnlyAfterRecovery(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService, registration := paperPositionFixture(t, pool, "rwa.mint-failure", "2")
	chain := newFakeRwaProvider()
	chain.failNextMint = true
	service := newRwaService(t, pool, paperService, paperService, chain)
	minted, err := service.Mint(t.Context(), rwaservice.MintCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "rwa-rwa-rwa-0003",
		AssetID: rwaAssetID, Quantity: money.MustParse("1"), CustodyMode: rwaservice.CustodyVault,
	})
	if !errors.Is(err, rwaservice.ErrOperationUnknown) || minted.Status != "RECOVERY_REQUIRED" {
		t.Fatalf("mint=%+v err=%v", minted, err)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM securities.position_reservations WHERE status = 'ACTIVE'")
	if err := service.RecoverOperation(t.Context(), rwaservice.RecoverCommand{OperationID: minted.OperationID, Actor: rwaservice.AdminActor{ID: "ops-recovery", Role: rwaservice.AdminRoleOps}}); err != nil {
		t.Fatal(err)
	}
	mints, _ := service.ListMints(t.Context(), registration.AccessToken)
	if mints[0].Status != "FAILED_RECOVERED" {
		t.Fatalf("mint failure was not recovered: %+v", mints[0])
	}
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM securities.position_reservations WHERE status = 'ACTIVE'")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM audit.events WHERE action = 'rwa.mint.failed_recovered'")
}

func TestRwaExternalAddressProofCoolingAndExternalCustody(t *testing.T) {
	pool := newFinancialTestPool(t)
	paperService, registration := paperPositionFixture(t, pool, "rwa.external-address", "2")
	chain := newFakeRwaProvider()
	verifier, err := rwaprovider.NewLocalAddressVerifier(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-25 * time.Hour)
	service, err := rwaservice.NewService(rwaservice.Dependencies{
		Database: pool, Environment: config.EnvironmentTest, Resolver: rwaTestResolver{service: paperService},
		Custodian: paperService, Provider: chain, AddressVerifier: verifier,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	address, err := service.RegisterExternalAddress(t.Context(), rwaservice.RegisterAddressCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "rwa-rwa-rwa-0004",
		Address: "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC", ProofReference: "sim-proof:v1-evidence",
	})
	if err != nil || address.Status != "RISK_REVIEW" {
		t.Fatalf("address proof result=%+v err=%v", address, err)
	}
	address, err = service.ReviewExternalAddress(t.Context(), rwaservice.ReviewAddressCommand{
		AddressID: address.ID, Approve: true, ReasonCode: "EVIDENCE_REVIEWED",
		Actor: rwaservice.AdminActor{ID: "risk-reviewer-1", Role: rwaservice.AdminRoleRisk},
	})
	if err != nil || address.Status != "COOLING" || address.CoolingEndsAt.IsZero() {
		t.Fatalf("address review result=%+v err=%v", address, err)
	}
	if _, err := service.ActivateExternalAddress(t.Context(), registration.AccessToken, address.ID); !errors.Is(err, rwaservice.ErrInvalidState) {
		t.Fatalf("expected cooling to block early activation, got %v", err)
	}
	now = time.Now().UTC()
	address, err = service.ActivateExternalAddress(t.Context(), registration.AccessToken, address.ID)
	if err != nil || address.Status != "ACTIVE" {
		t.Fatalf("address activation result=%+v err=%v", address, err)
	}
	minted, err := service.Mint(t.Context(), rwaservice.MintCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "rwa-rwa-rwa-0005",
		AssetID: rwaAssetID, Quantity: money.MustParse("1"), CustodyMode: rwaservice.CustodyExternal,
		ExternalAddressID: address.ID,
	})
	if err != nil || minted.Status != "MINTED" || minted.DestinationAddress != address.Address {
		t.Fatalf("external custody mint=%+v err=%v", minted, err)
	}
	redeemed, err := service.Redeem(t.Context(), rwaservice.RedeemCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: "rwa-rwa-rwa-0006", MintID: minted.ID,
	})
	if err != nil || redeemed.Status != "COMPLETED" {
		t.Fatalf("external custody redemption=%+v err=%v", redeemed, err)
	}
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM compliance.cases WHERE case_type = 'RWA_ADDRESS_REVIEW'")
	assertCount(t, pool, 0, "SELECT COUNT(*) FROM securities.position_reservations WHERE status = 'ACTIVE'")
	assertCount(t, pool, 1, "SELECT COUNT(*) FROM audit.events WHERE action = 'rwa.address.active'")
}

func paperPositionFixture(t *testing.T, pool *pgxpool.Pool, fixtureID, quantity string) (*securities.Service, securities.Registration) {
	t.Helper()
	service := newPaperService(t, pool, nil, 0)
	registration, err := service.RegisterFixture(t.Context(), fixtureID)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := service.SubmitOrder(t.Context(), securities.SubmitOrderCommand{
		AccessToken: registration.AccessToken, IdempotencyKey: fixtureID + "-buy-0001", Symbol: "AAPL",
		Side: broker.SideBuy, OrderType: broker.OrderTypeMarket, TimeInForce: broker.TimeInForceDay,
		Quantity: money.MustParse(quantity),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdvanceReplay(t.Context(), securities.ActionCommand{AccessToken: registration.AccessToken, IdempotencyKey: fixtureID + "-replay-0001", OrderID: partial.ID}); err != nil {
		t.Fatal(err)
	}
	return service, registration
}

func newRwaService(t *testing.T, pool *pgxpool.Pool, paperService *securities.Service, custodian rwaservice.ShareCustodian, chain rwaprovider.Adapter) *rwaservice.Service {
	t.Helper()
	verifier, _ := rwaprovider.NewLocalAddressVerifier(config.EnvironmentTest)
	service, err := rwaservice.NewService(rwaservice.Dependencies{
		Database: pool, Environment: config.EnvironmentTest, Resolver: rwaTestResolver{service: paperService},
		Custodian: custodian, Provider: chain, AddressVerifier: verifier,
		Now: func() time.Time { return time.Date(2026, time.July, 19, 16, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type rwaTestResolver struct{ service *securities.Service }

func (resolver rwaTestResolver) ResolveSession(ctx context.Context, token string) (rwaservice.CustomerSession, error) {
	account, err := resolver.service.ResolveSession(ctx, token)
	if err != nil {
		return rwaservice.CustomerSession{}, err
	}
	return rwaservice.CustomerSession{PaperAccountID: account.ID, CustomerReference: account.CustomerReference, CashLedgerAccountID: account.CashLedgerAccountID}, nil
}

type failingShareCustodian struct {
	service         *securities.Service
	failNextRelease bool
}

func (custodian *failingShareCustodian) ReserveSettledShares(ctx context.Context, tx pgx.Tx, command securities.ReserveSharesCommand) (securities.ShareReservation, error) {
	return custodian.service.ReserveSettledShares(ctx, tx, command)
}

func (custodian *failingShareCustodian) ReleaseSettledShares(ctx context.Context, tx pgx.Tx, reservationID, idempotencyKey string, actor ledger.Actor, effectiveAt time.Time) (securities.ShareReservation, error) {
	if custodian.failNextRelease {
		custodian.failNextRelease = false
		return securities.ShareReservation{}, errors.New("simulated release database failure")
	}
	return custodian.service.ReleaseSettledShares(ctx, tx, reservationID, idempotencyKey, actor, effectiveAt)
}

type fakeRwaProvider struct {
	mutex        sync.Mutex
	executed     map[string]bool
	supply       money.Decimal
	failNextMint bool
}

func newFakeRwaProvider() *fakeRwaProvider     { return &fakeRwaProvider{executed: make(map[string]bool)} }
func (provider *fakeRwaProvider) Name() string { return "local-anvil-rwa-simulator" }
func (provider *fakeRwaProvider) Capabilities(context.Context) (rwaprovider.Capability, error) {
	return rwaprovider.Capability{Provider: provider.Name(), ChainName: "BASE_ANVIL", ChainID: rwaprovider.BaseAnvilChainID, Simulated: true, Permissioned: true, WholeSharesOnly: true, ForcedRedemption: true, OperationReplay: true, SimulationMessage: rwaservice.SimulationOnly}, nil
}
func (provider *fakeRwaProvider) Health(context.Context) (rwaprovider.Health, error) {
	return rwaprovider.Health{Status: "SIMULATED", ChainID: rwaprovider.BaseAnvilChainID, BlockNumber: "0x1"}, nil
}
func (provider *fakeRwaProvider) Mint(_ context.Context, request rwaprovider.OperationRequest) (rwaprovider.OperationResult, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if provider.failNextMint {
		provider.failNextMint = false
		return rwaprovider.OperationResult{OperationID: request.OperationID}, rwaprovider.ErrChainReverted
	}
	provider.executed[request.OperationID] = true
	provider.supply, _ = provider.supply.Add(request.Quantity)
	return fakeOperationResult(request.OperationID), nil
}
func (provider *fakeRwaProvider) Burn(_ context.Context, request rwaprovider.OperationRequest) (rwaprovider.OperationResult, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.executed[request.OperationID] = true
	provider.supply, _ = provider.supply.Sub(request.Quantity)
	return fakeOperationResult(request.OperationID), nil
}
func (provider *fakeRwaProvider) ForcedRedemption(ctx context.Context, request rwaprovider.OperationRequest) (rwaprovider.OperationResult, error) {
	return provider.Burn(ctx, request)
}
func (provider *fakeRwaProvider) Operation(_ context.Context, _, operationID string) (rwaprovider.OperationStatus, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return rwaprovider.OperationStatus{OperationID: operationID, Executed: provider.executed[operationID], BlockNumber: "0x1"}, nil
}
func (provider *fakeRwaProvider) TotalSupply(context.Context, string) (money.Decimal, string, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.supply, "0x1", nil
}
func (provider *fakeRwaProvider) SetPermission(context.Context, string, string, bool) (rwaprovider.OperationResult, error) {
	return rwaprovider.OperationResult{Confirmed: true}, nil
}

func fakeOperationResult(operationID string) rwaprovider.OperationResult {
	txHash := "0x" + operationID
	return rwaprovider.OperationResult{OperationID: operationID, ExternalEventID: txHash, TransactionHash: txHash, Confirmed: true, Payload: []byte(`{"status":"0x1"}`)}
}

func mustPool(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
