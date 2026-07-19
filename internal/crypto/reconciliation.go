package crypto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	compliancestore "github.com/KDTikkly/Cytisus/internal/compliance/store"
	"github.com/KDTikkly/Cytisus/internal/crypto/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) Reconcile(ctx context.Context, command ReconcileCommand) (ReconciliationRun, error) {
	command.Asset = normalizedAsset(command.Asset)
	if command.Actor.ID == "" || command.CustomerReference == "" || command.Asset == "" || !authorizedReconciliationRole(command.Actor.Role) {
		return ReconciliationRun{}, ErrAdminUnauthorized
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("begin crypto reconciliation: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	profile, err := queries.GetCustomerProfileForUpdate(ctx, command.CustomerReference)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReconciliationRun{}, ErrAssetNotFound
	}
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("lock crypto reconciliation profile: %w", err)
	}
	if err := service.validateAsset(ctx, queries, command.Asset); err != nil {
		return ReconciliationRun{}, err
	}
	ledgerAmount := money.Zero()
	accounts, err := queries.GetAssetLedgerAccounts(ctx, store.GetAssetLedgerAccountsParams{
		CustomerReference: profile.CustomerReference, AssetSymbol: command.Asset,
	})
	if err == nil {
		ledgerAmount, err = ledger.NewService(tx).Balance(ctx, accounts.CustomerLedgerAccountID.String(), money.Currency(command.Asset), ledger.DimensionSettled)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ReconciliationRun{}, fmt.Errorf("get crypto reconciliation balance: %w", err)
	}
	custodyAmount := ledgerAmount
	if command.CustodyAmount != nil {
		if command.CustodyAmount.Sign() < 0 {
			return ReconciliationRun{}, ErrInvalidCommand
		}
		custodyAmount = *command.CustodyAmount
	}
	difference, err := ledgerAmount.Sub(custodyAmount)
	if err != nil {
		return ReconciliationRun{}, err
	}
	status := "MATCHED"
	caseID := pgtype.UUID{}
	runID, err := newUUID()
	if err != nil {
		return ReconciliationRun{}, err
	}
	if !difference.IsZero() {
		status = "DIFFERENCE"
		caseID, err = newUUID()
		if err != nil {
			return ReconciliationRun{}, err
		}
		nextAction := "Operations must compare the simulated custody report with immutable Ledger entries."
		complianceQueries := compliancestore.New(tx)
		if _, err := complianceQueries.CreateCase(ctx, compliancestore.CreateCaseParams{
			ID: caseID, CustomerReference: profile.CustomerReference, CaseType: "TRANSACTION_MONITORING_ALERT",
			ResourceType: "crypto.reconciliation", ResourceID: runID.String(), Status: "OPEN",
			ReasonCode: "CRYPTO_RECONCILIATION_DIFFERENCE", NextAction: nextAction, PolicyVersion: profile.PolicyVersion,
		}); err != nil {
			return ReconciliationRun{}, fmt.Errorf("create crypto reconciliation case: %w", err)
		}
		metadata, _ := json.Marshal(map[string]string{"asset": command.Asset, "difference": difference.String()})
		if _, err := complianceQueries.InsertCaseEvent(ctx, compliancestore.InsertCaseEventParams{
			CaseID: caseID, ToStatus: "OPEN", ActorType: "SYSTEM", ActorID: "crypto-reconciliation",
			ReasonCode: "CRYPTO_RECONCILIATION_DIFFERENCE", Metadata: metadata,
		}); err != nil {
			return ReconciliationRun{}, err
		}
	}
	created, err := queries.CreateReconciliationRun(ctx, store.CreateReconciliationRunParams{
		ID: runID, CustomerReference: profile.CustomerReference, AssetSymbol: command.Asset,
		LedgerAmount: ledgerAmount, CustodyAmount: custodyAmount, Difference: difference,
		Status: status, ComplianceCaseID: caseID, PolicyVersion: profile.PolicyVersion,
	})
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("create crypto reconciliation run: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{
		"asset": command.Asset, "ledger_amount": ledgerAmount.String(), "custody_amount": custodyAmount.String(), "difference": difference.String(),
	})
	payload, _ := json.Marshal(map[string]string{"reconciliation_id": runID.String(), "status": status})
	if err := recordMutation(ctx, tx, mutation{
		Action: "crypto.reconciliation.completed", ResourceType: "crypto.reconciliation", ResourceID: runID.String(),
		ActorType: "ADMIN", ActorID: command.Actor.ID, CorrelationID: runID,
		Metadata: metadata, AggregateType: "crypto.reconciliation", AggregateID: runID.String(),
		EventType: "crypto.reconciliation.completed", Payload: payload,
	}); err != nil {
		return ReconciliationRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconciliationRun{}, fmt.Errorf("commit crypto reconciliation: %w", err)
	}
	result := ReconciliationRun{
		ID: created.ID.String(), Asset: created.AssetSymbol, LedgerAmount: created.LedgerAmount,
		CustodyAmount: created.CustodyAmount, Difference: created.Difference, Status: created.Status,
		ComplianceCase: created.ComplianceCaseID.String(), CompletedAt: created.CompletedAt.Time.UTC(),
	}
	if status == "DIFFERENCE" {
		return result, ErrReconciliationDifference
	}
	return result, nil
}

func authorizedReconciliationRole(role string) bool {
	switch role {
	case "OPERATIONS", "RISK_ANALYST", "ADMIN":
		return true
	default:
		return false
	}
}
