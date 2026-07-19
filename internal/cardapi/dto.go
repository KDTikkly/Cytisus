package cardapi

import (
	"time"

	"github.com/KDTikkly/Cytisus/internal/card"
	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/notification"
)

const timeFormat = time.RFC3339Nano

func profileDTO(value card.Profile) map[string]any {
	cards := make([]any, 0, len(value.Cards))
	for _, item := range value.Cards {
		cards = append(cards, cardDTO(item))
	}
	return map[string]any{
		"customer_reference": value.CustomerReference, "repayment_mode": value.RepaymentMode,
		"spending_status": value.SpendingStatus, "spending_status_reason": optionalString(value.SpendingStatusReason),
		"policy_version": value.PolicyVersion, "cards": cards, "mode": "SIMULATED",
	}
}

func cardDTO(value card.Card) map[string]any {
	return map[string]any{
		"id": value.ID, "card_type": value.Type, "status": value.Status, "display_name": value.DisplayName,
		"last4": value.Last4, "pin_set": value.PINSet, "provider": value.Provider,
		"provider_card_reference": value.ProviderCardReference,
		"apple_wallet_status":     value.AppleWalletStatus, "google_wallet_status": value.GoogleWalletStatus,
		"replacement_for_card_id": optionalString(value.ReplacementForCardID), "policy_version": value.PolicyVersion,
		"replayed": value.Replayed, "created_at": value.CreatedAt.Format(timeFormat), "updated_at": value.UpdatedAt.Format(timeFormat),
	}
}

func spendingPowerDTO(value card.SpendingPower) map[string]any {
	drivers := make([]any, 0, len(value.Drivers))
	for _, driver := range value.Drivers {
		drivers = append(drivers, map[string]any{
			"symbol": driver.Symbol, "display_name": driver.DisplayName, "asset_class": driver.AssetClass,
			"quantity": driver.Quantity.String(), "reference_price_usd": driver.ReferencePrice.String(),
			"market_value_usd": driver.MarketValueUSD.String(), "eligible_value_usd": driver.EligibleValueUSD.String(),
			"haircut_rate": driver.HaircutRate.String(), "quote_status": driver.QuoteStatus, "market_status": driver.MarketStatus,
			"eligible": driver.Eligible, "exclusion_reason": optionalString(driver.ExclusionReason),
			"observed_at": driver.ObservedAt.Format(timeFormat),
		})
	}
	return map[string]any{
		"cash_eligible_usd": value.CashEligibleUSD.String(), "collateral_eligible_usd": value.CollateralEligibleUSD.String(),
		"gross_usd": value.GrossUSD.String(), "outstanding_holds_usd": value.OutstandingHoldsUSD.String(),
		"receivable_usd": value.ReceivableUSD.String(), "available_usd": value.AvailableUSD.String(),
		"absolute_cap_usd": value.AbsoluteCapUSD.String(), "provider_cap_usd": value.ProviderCapUSD.String(),
		"repayment_mode": value.RepaymentMode, "spending_status": value.SpendingStatus,
		"primary_explanation": value.PrimaryExplanation, "policy_version": value.PolicyVersion,
		"drivers": drivers, "calculated_at": value.CalculatedAt.Format(timeFormat), "mode": "SIMULATED",
	}
}

func mandateDTO(value card.AutoSellMandate) map[string]any {
	assets := make([]any, 0, len(value.Assets))
	for _, asset := range value.Assets {
		assets = append(assets, map[string]any{
			"priority": asset.Priority, "symbol": asset.Symbol, "minimum_retain_quantity": asset.MinimumRetainQuantity.String(),
		})
	}
	return map[string]any{
		"id": value.ID, "enabled": value.Enabled, "allow_fractional": value.AllowFractional,
		"daily_max_usd": value.DailyMaxUSD.String(), "valid_until": value.ValidUntil.Format(timeFormat),
		"policy_version": value.PolicyVersion, "assets": assets, "updated_at": value.UpdatedAt.Format(timeFormat),
	}
}

func authorizationDTO(value card.Authorization) map[string]any {
	return map[string]any{
		"id": value.ID, "card_id": value.CardID, "external_authorization_id": value.ExternalAuthorizationID,
		"merchant_name": value.MerchantName, "merchant_category_code": value.MerchantCategoryCode,
		"merchant_amount": value.MerchantAmount.String(), "merchant_currency": value.MerchantCurrency,
		"authorization_fx_rate": value.AuthorizationFXRate.String(), "fx_markup_rate": value.FXMarkupRate.String(),
		"authorized_usd": value.AuthorizedUSD.String(), "status": value.Status, "offline": value.Offline,
		"entry_mode": value.EntryMode, "decline_code": optionalString(value.DeclineCode),
		"hold_ledger_transaction_id": optionalString(value.HoldLedgerTransactionID),
		"captured_usd":               value.CapturedUSD.String(), "reversed_usd": value.ReversedUSD.String(),
		"policy_version": value.PolicyVersion, "replayed": value.Replayed,
		"occurred_at": value.OccurredAt.Format(timeFormat), "created_at": value.CreatedAt.Format(timeFormat), "mode": "SIMULATED",
	}
}

func captureDTO(value card.Capture) map[string]any {
	executions := make([]any, 0, len(value.AutoSellExecutions))
	for _, execution := range value.AutoSellExecutions {
		executions = append(executions, map[string]any{
			"id": execution.ID, "sequence": execution.Sequence, "symbol": execution.Symbol,
			"requested_quantity": execution.RequestedQuantity.String(), "protected_limit_price": execution.ProtectedLimitPrice.String(),
			"execution_price": execution.ExecutionPrice.String(), "filled_quantity": execution.FilledQuantity.String(),
			"proceeds_usd": execution.ProceedsUSD.String(), "status": execution.Status,
			"failure_code": optionalString(execution.FailureCode), "ledger_transaction_id": optionalString(execution.LedgerTransactionID),
		})
	}
	return map[string]any{
		"id": value.ID, "authorization_id": value.AuthorizationID, "external_capture_id": value.ExternalCaptureID,
		"merchant_amount": value.MerchantAmount.String(), "merchant_currency": value.MerchantCurrency,
		"clearing_fx_rate": value.ClearingFXRate.String(), "fx_markup_rate": value.FXMarkupRate.String(),
		"settled_usd": value.SettledUSD.String(), "tip_usd": value.TipUSD.String(),
		"hold_released_usd": value.HoldReleasedUSD.String(), "cash_repaid_usd": value.CashRepaidUSD.String(),
		"auto_sell_repaid_usd": value.AutoSellRepaidUSD.String(), "refunded_usd": value.RefundedUSD.String(),
		"status": value.Status, "receivable_ledger_transaction_id": value.ReceivableLedgerTransactionID,
		"repayment_ledger_transaction_id": optionalString(value.RepaymentLedgerTransactionID),
		"auto_sell_executions":            executions, "replayed": value.Replayed,
		"occurred_at": value.OccurredAt.Format(timeFormat), "created_at": value.CreatedAt.Format(timeFormat), "mode": "SIMULATED",
	}
}

func refundDTO(value card.Refund) map[string]any {
	return map[string]any{
		"id": value.ID, "capture_id": value.CaptureID, "external_refund_id": value.ExternalRefundID,
		"refund_usd": value.RefundUSD.String(), "receivable_reduction_usd": value.ReceivableReductionUSD.String(),
		"cash_credit_usd": value.CashCreditUSD.String(), "ledger_transaction_id": value.LedgerTransactionID,
		"replayed": value.Replayed, "occurred_at": value.OccurredAt.Format(timeFormat), "mode": "SIMULATED",
	}
}

func disputeDTO(value card.Dispute) map[string]any {
	return map[string]any{
		"id": value.ID, "capture_id": value.CaptureID, "customer_reference": value.CustomerReference,
		"amount_usd": value.AmountUSD.String(), "reason_code": value.ReasonCode, "status": value.Status,
		"outcome": optionalString(value.Outcome), "compliance_case_id": value.ComplianceCaseID,
		"ledger_transaction_id": optionalString(value.LedgerTransactionID),
		"opened_at":             value.OpenedAt.Format(timeFormat), "resolved_at": optionalTime(value.ResolvedAt),
	}
}

func statementDTO(value card.Statement) map[string]any {
	return map[string]any{
		"id": value.ID, "customer_reference": value.CustomerReference, "period_start": value.PeriodStart,
		"period_end": value.PeriodEnd, "amount_due_usd": value.AmountDueUSD.String(), "status": value.Status,
		"due_at": value.DueAt.Format(timeFormat), "paid_at": optionalTime(value.PaidAt),
		"repayment_ledger_transaction_id": optionalString(value.RepaymentLedgerTransactionID),
		"created_at":                      value.CreatedAt.Format(timeFormat),
	}
}

func reconciliationDTO(value card.ReconciliationRun) map[string]any {
	return map[string]any{
		"id": value.ID, "customer_reference": value.CustomerReference,
		"ledger_hold_usd": value.LedgerHoldUSD.String(), "provider_hold_usd": value.ProviderHoldUSD.String(),
		"ledger_receivable_usd": value.LedgerReceivableUSD.String(), "provider_receivable_usd": value.ProviderReceivableUSD.String(),
		"difference_usd": value.DifferenceUSD.String(), "status": value.Status,
		"compliance_case_id": optionalString(value.ComplianceCaseID), "completed_at": value.CompletedAt.Format(timeFormat),
	}
}

func notificationDTO(value notification.InboxItem) map[string]any {
	return map[string]any{
		"id": value.ID, "notification_event_id": value.NotificationEventID, "category": value.Category,
		"event_type": value.EventType, "title_key": value.TitleKey, "body_key": value.BodyKey,
		"resource_type": value.ResourceType, "resource_id": value.ResourceID,
		"action_path": optionalString(value.ActionPath), "delivery_status": value.DeliveryStatus,
		"created_at": value.CreatedAt.Format(timeFormat),
	}
}

func capabilitiesDTO(value cardprovider.Capabilities) map[string]any {
	return map[string]any{
		"mode": value.Mode, "virtual_cards": value.VirtualCards, "physical_cards": value.PhysicalCards,
		"metal_cards": value.MetalCards, "nfc_terminal": value.NFCTerminal,
		"apple_wallet_status": value.AppleWalletStatus, "google_wallet_status": value.GoogleWalletStatus,
		"deterministic_replay": value.DeterministicReplay,
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
