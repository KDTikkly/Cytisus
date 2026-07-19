package bankingapi

import (
	"time"

	"github.com/KDTikkly/Cytisus/internal/banking"
)

const timeFormat = time.RFC3339Nano

func bankAccountDTO(account banking.BankAccount) map[string]any {
	return map[string]any{
		"id": account.ID, "provider": account.Provider, "external_account_reference": account.ExternalAccountReference,
		"rail_support": account.RailSupport, "owner_relation": account.OwnerRelation, "ownership_status": account.OwnershipStatus,
		"status": account.Status, "preferred_for_withdrawal": account.PreferredForWithdrawal,
		"successfully_funded_at": optionalTime(account.SuccessfullyFundedAt), "cooling_until": optionalTime(account.CoolingUntil),
		"created_at": account.CreatedAt.Format(timeFormat), "mode": "SIMULATED",
	}
}

func fundingDTO(transfer banking.FundingTransfer) map[string]any {
	return map[string]any{
		"id": transfer.ID, "bank_account_id": transfer.BankAccountID, "rail": transfer.Rail,
		"amount": transfer.Amount.String(), "currency": "USD", "status": transfer.Status,
		"provider_transfer_id": transfer.ProviderTransferID, "reason_code": optionalString(transfer.ReasonCode),
		"pending": transfer.Pending, "settled": transfer.Settled, "replayed": transfer.Replayed,
		"created_at": transfer.CreatedAt.Format(timeFormat), "updated_at": transfer.UpdatedAt.Format(timeFormat), "mode": "SIMULATED",
	}
}

func withdrawalDTO(withdrawal banking.Withdrawal) map[string]any {
	return map[string]any{
		"id": withdrawal.ID, "bank_account_id": withdrawal.BankAccountID, "amount": withdrawal.Amount.String(), "currency": "USD",
		"status": withdrawal.Status, "reason_code": withdrawal.ReasonCode, "next_action": withdrawal.NextAction,
		"compliance_case_id": optionalString(withdrawal.ComplianceCaseID), "cooling_until": optionalTime(withdrawal.CoolingUntil),
		"provider_transfer_id": withdrawal.ProviderTransferID, "replayed": withdrawal.Replayed,
		"created_at": withdrawal.CreatedAt.Format(timeFormat), "updated_at": withdrawal.UpdatedAt.Format(timeFormat), "mode": "SIMULATED",
	}
}

func caseDTO(value banking.ComplianceCase) map[string]any {
	return map[string]any{
		"id": value.ID, "customer_reference": value.CustomerRef, "case_type": value.CaseType,
		"resource_type": value.ResourceType, "resource_id": value.ResourceID, "status": value.Status,
		"reason_code": value.ReasonCode, "next_action": value.NextAction, "policy_version": value.PolicyVersion,
		"version": value.Version, "created_at": value.CreatedAt.Format(timeFormat), "updated_at": value.UpdatedAt.Format(timeFormat),
	}
}

func proposalDTO(value banking.ReviewProposal) map[string]any {
	return map[string]any{
		"id": value.ID, "case_id": value.CaseID, "action": value.Action, "maker_id": value.MakerID,
		"maker_role": value.MakerRole, "reason_code": value.ReasonCode, "ticket_reference": value.TicketReference,
		"evidence_reference": value.EvidenceReference, "status": value.Status, "checker_id": optionalString(value.CheckerID),
		"checker_role": optionalString(value.CheckerRole), "decision_reason": optionalString(value.DecisionReason),
		"created_at": value.CreatedAt.Format(timeFormat), "decided_at": optionalTime(value.DecidedAt),
	}
}

func reconciliationDTO(value banking.ReconciliationRun) map[string]any {
	return map[string]any{
		"id": value.ID, "status": value.Status, "ledger_amount": value.LedgerAmount.String(),
		"provider_amount": value.ProviderAmount.String(), "difference": value.Difference.String(),
		"compliance_case_id": optionalString(value.CaseID), "started_at": value.StartedAt.Format(timeFormat),
		"completed_at": value.CompletedAt.Format(timeFormat),
	}
}

func optionalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func optionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(timeFormat)
}
