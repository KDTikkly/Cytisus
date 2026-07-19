package card

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) Reconcile(ctx context.Context, command ReconcileCommand) (ReconciliationRun, error) {
	command.CustomerReference = strings.TrimSpace(command.CustomerReference)
	if !authorizedReconciliationAdmin(command.Actor) || command.CustomerReference == "" ||
		(command.ProviderHoldUSD != nil && command.ProviderHoldUSD.Sign() < 0) ||
		(command.ProviderReceivableUSD != nil && command.ProviderReceivableUSD.Sign() < 0) {
		return ReconciliationRun{}, ErrAdminUnauthorized
	}
	identifier, err := newUUID()
	if err != nil {
		return ReconciliationRun{}, err
	}
	startedAt := service.now().UTC()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ReconciliationRun{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	profile, err := queries.GetCustomerProfileForUpdate(ctx, command.CustomerReference)
	if err != nil {
		return ReconciliationRun{}, ErrInvalidCommand
	}
	ledgerService := ledger.NewService(tx)
	ledgerHold, err := ledgerService.Balance(ctx, profile.ReceivableLedgerAccountID.String(), "USD", ledger.DimensionHeld)
	if err != nil {
		return ReconciliationRun{}, err
	}
	ledgerReceivable, err := ledgerService.Balance(ctx, profile.ReceivableLedgerAccountID.String(), "USD", ledger.DimensionReceivable)
	if err != nil {
		return ReconciliationRun{}, err
	}
	ledgerHold, ledgerReceivable = positiveRemainder(ledgerHold), positiveRemainder(ledgerReceivable)
	providerHold, providerReceivable := ledgerHold, ledgerReceivable
	if command.ProviderHoldUSD != nil {
		providerHold = *command.ProviderHoldUSD
	}
	if command.ProviderReceivableUSD != nil {
		providerReceivable = *command.ProviderReceivableUSD
	}
	holdDifference, err := providerHold.Sub(ledgerHold)
	if err != nil {
		return ReconciliationRun{}, err
	}
	receivableDifference, err := providerReceivable.Sub(ledgerReceivable)
	if err != nil {
		return ReconciliationRun{}, err
	}
	difference, err := holdDifference.Abs().Add(receivableDifference.Abs())
	if err != nil {
		return ReconciliationRun{}, err
	}
	status := "COMPLETED_WITHOUT_DIFFERENCE"
	caseID := pgtype.UUID{}
	if difference.IsPositive() {
		status = "COMPLETED_WITH_DIFFERENCES"
		complianceCase, caseErr := createComplianceCase(
			ctx, tx, profile.CustomerReference, "CARD_RECONCILIATION", "card.reconciliation", identifier.String(),
			"CARD_PROVIDER_LEDGER_DIFFERENCE", "An authorized operations reviewer must investigate; no automatic balance adjustment is permitted.",
		)
		if caseErr != nil {
			return ReconciliationRun{}, caseErr
		}
		caseID = complianceCase.ID
	}
	completedAt := service.now().UTC()
	created, err := queries.InsertReconciliationRun(ctx, store.InsertReconciliationRunParams{
		ID: identifier, CustomerReference: profile.CustomerReference,
		LedgerHoldUsd: ledgerHold, ProviderHoldUsd: providerHold,
		LedgerReceivableUsd: ledgerReceivable, ProviderReceivableUsd: providerReceivable,
		DifferenceUsd: difference, Status: status, ComplianceCaseID: caseID,
		StartedAt: timestamptz(startedAt), CompletedAt: timestamptz(completedAt),
	})
	if err != nil {
		return ReconciliationRun{}, err
	}
	payload, _ := json.Marshal(map[string]string{
		"reconciliation_run_id": identifier.String(), "difference_usd": difference.String(), "status": status,
	})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.reconciliation.completed", ResourceType: "card.reconciliation", ResourceID: identifier.String(),
		ActorType: "ADMIN", ActorID: command.Actor.ID, CorrelationID: identifier, Metadata: payload,
		AggregateType: "card.reconciliation", AggregateID: identifier.String(), EventType: "card.reconciliation.completed", Payload: payload,
	}); err != nil {
		return ReconciliationRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconciliationRun{}, err
	}
	return reconciliationFromStore(created), nil
}

func (service *Service) ListReconciliationRuns(ctx context.Context, actor AdminActor, customerReference string, pageSize int32) ([]ReconciliationRun, error) {
	if !authorizedReconciliationAdmin(actor) {
		return nil, ErrAdminUnauthorized
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(service.database).ListReconciliationRuns(ctx, store.ListReconciliationRunsParams{
		CustomerFilter: strings.TrimSpace(customerReference), PageSize: pageSize,
	})
	if err != nil {
		return nil, err
	}
	result := make([]ReconciliationRun, 0, len(rows))
	for _, row := range rows {
		result = append(result, reconciliationFromStore(row))
	}
	return result, nil
}

func reconciliationFromStore(value store.CardReconciliationRun) ReconciliationRun {
	return ReconciliationRun{
		ID: value.ID.String(), CustomerReference: value.CustomerReference,
		LedgerHoldUSD: value.LedgerHoldUsd, ProviderHoldUSD: value.ProviderHoldUsd,
		LedgerReceivableUSD: value.LedgerReceivableUsd, ProviderReceivableUSD: value.ProviderReceivableUsd,
		DifferenceUSD: value.DifferenceUsd, Status: value.Status,
		ComplianceCaseID: value.ComplianceCaseID.String(), CompletedAt: value.CompletedAt.Time.UTC(),
	}
}

func authorizedReconciliationAdmin(actor AdminActor) bool {
	if actor.ID == "" {
		return false
	}
	switch actor.Role {
	case "OPERATIONS", "RISK_ANALYST", "AUDITOR", "ADMIN":
		return true
	default:
		return false
	}
}
