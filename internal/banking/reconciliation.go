package banking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KDTikkly/Cytisus/internal/banking/store"
	"github.com/jackc/pgx/v5"
)

func (service *Service) ReconcileCustomer(ctx context.Context, command ReconcileCustomerCommand) (ReconciliationRun, error) {
	if !reconciliationRoleAllowed(command.Actor.Role) || command.Actor.ID == "" || command.CustomerReference == "" {
		return ReconciliationRun{}, ErrAdminUnauthorized
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("begin bank reconciliation: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	profile, err := queries.GetCustomerProfileForUpdate(ctx, command.CustomerReference)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReconciliationRun{}, ErrUnauthorized
	}
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("lock reconciliation profile: %w", err)
	}
	ledgerAmount, err := queries.SumLedgerBankingSettled(ctx, profile.CashLedgerAccountID)
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("sum bank Ledger activity: %w", err)
	}
	providerFunding, err := queries.SumProviderSettledFunding(ctx, command.CustomerReference)
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("sum provider funding: %w", err)
	}
	providerWithdrawals, err := queries.SumProviderSettledWithdrawals(ctx, command.CustomerReference)
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("sum provider withdrawals: %w", err)
	}
	providerAmount, err := providerFunding.Sub(providerWithdrawals)
	if err != nil {
		return ReconciliationRun{}, err
	}
	if command.ProviderAmount != nil {
		providerAmount = *command.ProviderAmount
	}
	difference, err := ledgerAmount.Sub(providerAmount)
	if err != nil {
		return ReconciliationRun{}, err
	}
	runID, err := newUUID()
	if err != nil {
		return ReconciliationRun{}, err
	}
	started, err := queries.CreateReconciliationRun(ctx, store.CreateReconciliationRunParams{
		ID: runID, Scope: "CASH_VS_BANK_PROVIDER", PolicyVersion: PolicyVersion,
	})
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("create reconciliation run: %w", err)
	}
	status := "MATCHED"
	runStatus := "COMPLETED_WITHOUT_DIFFERENCE"
	var caseID string
	if !difference.IsZero() {
		status = "DIFFERENCE"
		runStatus = "COMPLETED_WITH_DIFFERENCES"
		complianceCase, caseErr := createCase(
			ctx, tx, command.CustomerReference, "TRANSACTION_MONITORING_ALERT", "bank.reconciliation", runID.String(),
			"BANK_RECONCILIATION_DIFFERENCE", "Operations must compare provider evidence and propose any adjustment through maker-checker.", "IN_REVIEW",
		)
		if caseErr != nil {
			return ReconciliationRun{}, caseErr
		}
		caseID = complianceCase.ID.String()
	}
	if _, err := queries.InsertReconciliationItem(ctx, store.InsertReconciliationItemParams{
		RunID: runID, CustomerReference: command.CustomerReference, LedgerAmount: ledgerAmount,
		ProviderAmount: providerAmount, Difference: difference, Status: status, ComplianceCaseID: uuidValue(caseID),
	}); err != nil {
		return ReconciliationRun{}, fmt.Errorf("insert reconciliation item: %w", err)
	}
	completed, err := queries.CompleteReconciliationRun(ctx, store.CompleteReconciliationRunParams{Status: runStatus, ID: runID})
	if err != nil {
		return ReconciliationRun{}, fmt.Errorf("complete reconciliation: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{
		"difference": difference.String(), "ledger_amount": ledgerAmount.String(), "provider_amount": providerAmount.String(), "status": runStatus,
	})
	if err := recordMutation(ctx, tx, mutation{
		Action: "bank.reconciliation.completed", ResourceType: "bank.reconciliation", ResourceID: runID.String(),
		ActorType: "ADMIN", ActorID: command.Actor.ID, Metadata: metadata,
		AggregateType: "bank.reconciliation", AggregateID: runID.String(), EventType: "bank.reconciliation.completed", Payload: metadata,
	}); err != nil {
		return ReconciliationRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReconciliationRun{}, fmt.Errorf("commit reconciliation: %w", err)
	}
	result := ReconciliationRun{
		ID: completed.ID.String(), Status: completed.Status, LedgerAmount: ledgerAmount, ProviderAmount: providerAmount,
		Difference: difference, CaseID: caseID, StartedAt: started.StartedAt.Time.UTC(), CompletedAt: completed.CompletedAt.Time.UTC(),
	}
	if !difference.IsZero() {
		return result, ErrReconciliationDifference
	}
	return result, nil
}

func reconciliationRoleAllowed(role string) bool {
	switch role {
	case "FINANCE", "OPERATIONS", "ADMIN":
		return true
	default:
		return false
	}
}
