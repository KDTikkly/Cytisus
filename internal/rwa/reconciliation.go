package rwa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	compliancestore "github.com/KDTikkly/Cytisus/internal/compliance/store"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/rwa/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var errDailyReconciliationExists = errors.New("daily RWA reconciliation already exists")

// RunDailyReconciliation is the worker entry point. It records at most one
// automatic run per enabled asset per UTC day; administrators may still run
// an explicit on-demand comparison at any time.
func (service *Service) RunDailyReconciliation(ctx context.Context) (int, error) {
	assets, err := store.New(service.database).ListAssets(ctx)
	if err != nil {
		return 0, fmt.Errorf("list RWA assets for daily reconciliation: %w", err)
	}
	dayStart := service.now().UTC().Truncate(24 * time.Hour)
	completed := 0
	var runErrors []error
	for _, asset := range assets {
		runs, listErr := store.New(service.database).ListReconciliationRuns(ctx, store.ListReconciliationRunsParams{AssetID: asset.ID, PageSize: 1})
		if listErr != nil {
			runErrors = append(runErrors, listErr)
			continue
		}
		if len(runs) > 0 && !runs[0].CompletedAt.Time.UTC().Before(dayStart) {
			continue
		}
		dailyKey := asset.ID.String() + ":" + service.now().UTC().Format(time.DateOnly)
		_, runErr := service.reconcile(ctx, ReconcileCommand{
			AssetID: asset.ID.String(), Actor: AdminActor{ID: "rwa-daily-reconciliation", Role: AdminRoleOps},
		}, dailyKey)
		if errors.Is(runErr, errDailyReconciliationExists) {
			continue
		}
		if runErr != nil {
			runErrors = append(runErrors, runErr)
			continue
		}
		completed++
	}
	return completed, errors.Join(runErrors...)
}

func (service *Service) Reconcile(ctx context.Context, command ReconcileCommand) (Reconciliation, error) {
	return service.reconcile(ctx, command, "")
}

func (service *Service) reconcile(ctx context.Context, command ReconcileCommand, dailyKey string) (Reconciliation, error) {
	if !authorizedAdmin(command.Actor) {
		return Reconciliation{}, ErrAdminUnauthorized
	}
	assetID, err := parseUUID(command.AssetID)
	if err != nil {
		return Reconciliation{}, ErrAssetNotFound
	}
	asset, err := store.New(service.database).GetAsset(ctx, assetID)
	if err != nil {
		return Reconciliation{}, ErrAssetNotFound
	}
	providerContext, cancel := service.withProviderTimeout(ctx)
	chainSupply, observedBlock, err := service.provider.TotalSupply(providerContext, asset.ContractAddress)
	cancel()
	if err != nil {
		return Reconciliation{}, fmt.Errorf("read RWA chain supply: %w", err)
	}
	runID, err := newUUID()
	if err != nil {
		return Reconciliation{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Reconciliation{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	lockedShares, err := queries.SumLockedSharesByAsset(ctx, assetID)
	if err != nil {
		return Reconciliation{}, err
	}
	difference, err := chainSupply.Sub(lockedShares)
	if err != nil {
		return Reconciliation{}, err
	}
	status := "BALANCED"
	caseID := pgtype.UUID{}
	if !difference.IsZero() {
		status = "DIFFERENCE"
		caseID, err = service.createReconciliationCase(ctx, tx, runID, difference)
		if err != nil {
			return Reconciliation{}, err
		}
	}
	created, err := queries.InsertReconciliationRun(ctx, store.InsertReconciliationRunParams{
		ID: runID, AssetID: assetID, ChainSupply: chainSupply, LockedShares: lockedShares,
		Difference: difference, Status: status, ComplianceCaseID: caseID, ObservedBlock: observedBlock,
		DailyKey: textValue(dailyKey),
	})
	if err != nil {
		var postgresError *pgconn.PgError
		if dailyKey != "" && errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "reconciliation_runs_daily_key_key" {
			return Reconciliation{}, errDailyReconciliationExists
		}
		return Reconciliation{}, err
	}
	payload, _ := json.Marshal(map[string]string{
		"chain_supply": chainSupply.String(), "locked_shares": lockedShares.String(),
		"difference": difference.String(), "status": status, "automatic_adjustment": "false",
	})
	if err := recordMutation(ctx, tx, mutation{
		Action: "rwa.reconciliation.completed", ResourceType: "rwa.reconciliation", ResourceID: runID.String(),
		ActorType: "ADMIN", ActorID: command.Actor.ID, CorrelationID: runID, Metadata: payload,
		AggregateType: "rwa.reconciliation", AggregateID: runID.String(), EventType: "rwa.reconciliation.completed", Payload: payload,
	}); err != nil {
		return Reconciliation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Reconciliation{}, err
	}
	return reconciliationFromStore(created), nil
}

func (service *Service) createReconciliationCase(ctx context.Context, tx pgx.Tx, runID pgtype.UUID, difference money.Decimal) (pgtype.UUID, error) {
	caseID, err := newUUID()
	if err != nil {
		return pgtype.UUID{}, err
	}
	created, err := compliancestore.New(tx).CreateCase(ctx, compliancestore.CreateCaseParams{
		ID: caseID, CustomerReference: "system:rwa-reconciliation", CaseType: "RWA_RECONCILIATION",
		ResourceType: "rwa.reconciliation", ResourceID: runID.String(), Status: "OPEN",
		ReasonCode: "RWA_SUPPLY_LOCK_DIFFERENCE", NextAction: "Investigate chain supply and locked-share records; no automatic balance or supply adjustment is permitted.",
		PolicyVersion: PolicyVersion,
	})
	if err != nil {
		return pgtype.UUID{}, err
	}
	metadata, _ := json.Marshal(map[string]string{"difference": difference.String(), "automatic_adjustment": "false"})
	if _, err := compliancestore.New(tx).InsertCaseEvent(ctx, compliancestore.InsertCaseEventParams{
		CaseID: created.ID, ToStatus: "OPEN", ActorType: "SYSTEM", ActorID: "rwa-reconciliation",
		ReasonCode: "RWA_SUPPLY_LOCK_DIFFERENCE", Metadata: metadata,
	}); err != nil {
		return pgtype.UUID{}, err
	}
	return created.ID, nil
}

func (service *Service) ListReconciliations(ctx context.Context, actor AdminActor, assetID string, pageSize int32) ([]Reconciliation, error) {
	if !authorizedAdmin(actor) {
		return nil, ErrAdminUnauthorized
	}
	identifier, err := parseUUID(assetID)
	if err != nil {
		return nil, ErrAssetNotFound
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(service.database).ListReconciliationRuns(ctx, store.ListReconciliationRunsParams{AssetID: identifier, PageSize: pageSize})
	if err != nil {
		return nil, err
	}
	result := make([]Reconciliation, 0, len(rows))
	for _, row := range rows {
		result = append(result, reconciliationFromStore(row))
	}
	return result, nil
}

func reconciliationFromStore(value store.RwaReconciliationRun) Reconciliation {
	return Reconciliation{
		ID: value.ID.String(), AssetID: value.AssetID.String(), ChainSupply: value.ChainSupply,
		LockedShares: value.LockedShares, Difference: value.Difference, Status: value.Status,
		ComplianceCaseID: value.ComplianceCaseID.String(), ObservedBlock: value.ObservedBlock,
		CompletedAt: value.CompletedAt.Time.UTC(),
	}
}
