package cryptoapi

import (
	"time"

	"github.com/KDTikkly/Cytisus/internal/crypto"
)

const timeFormat = time.RFC3339Nano

func assetDTO(asset crypto.Asset) map[string]any {
	networks := make([]any, 0, len(asset.Networks))
	for _, network := range asset.Networks {
		networks = append(networks, map[string]any{
			"code": network.Code, "display_name": network.DisplayName, "deposit_enabled": network.DepositEnabled,
			"withdrawal_enabled": network.WithdrawalEnabled, "native_deployment": network.NativeDeployment,
		})
	}
	return map[string]any{
		"symbol": asset.Symbol, "display_name": asset.DisplayName, "is_stablecoin": asset.IsStablecoin,
		"tradable": true, "precision": asset.Precision, "quote_currency": "USD", "networks": networks,
	}
}

func portfolioDTO(balance crypto.PortfolioBalance) map[string]any {
	return map[string]any{
		"asset": balance.Asset, "settled": balance.Settled.String(), "held": balance.Held.String(),
		"frozen": balance.Frozen.String(), "custody_model": "CUSTODIAL",
	}
}

func conversionDTO(value crypto.Conversion) map[string]any {
	legs := make([]any, 0, len(value.Legs))
	for _, leg := range value.Legs {
		legs = append(legs, legDTO(leg))
	}
	return map[string]any{
		"id": value.ID, "source_asset": value.SourceAsset, "destination_asset": value.DestinationAsset,
		"source_amount": value.SourceAmount.String(), "simulation_scenario": value.SimulationScenario,
		"status": value.Status, "reason_code": optionalString(value.ReasonCode), "next_action": value.NextAction,
		"policy_version": value.PolicyVersion, "compliance_case_id": optionalString(value.ComplianceCaseID),
		"legs": legs, "replayed": value.Replayed, "mode": "SIMULATED",
		"created_at": value.CreatedAt.Format(timeFormat), "updated_at": value.UpdatedAt.Format(timeFormat),
	}
}

func legDTO(value crypto.Leg) map[string]any {
	children := make([]any, 0, len(value.Children))
	for _, child := range value.Children {
		fills := make([]any, 0, len(child.Fills))
		for _, fill := range child.Fills {
			fills = append(fills, map[string]any{
				"id": fill.ID, "venue": fill.Venue, "external_fill_id": fill.ExternalFillID,
				"quantity": fill.Quantity.String(), "price": fill.Price.String(), "gross_usd": fill.GrossUSD.String(),
				"venue_fee_usd": fill.VenueFeeUSD.String(), "occurred_at": fill.OccurredAt.Format(timeFormat),
			})
		}
		children = append(children, map[string]any{
			"id": child.ID, "sequence": child.Sequence, "venue": child.Venue, "client_order_id": child.ClientOrderID,
			"provider_order_id": optionalString(child.ProviderOrderID), "requested_quantity": child.RequestedQuantity.String(),
			"filled_quantity": child.FilledQuantity.String(), "quote_bid": child.QuoteBid.String(), "quote_ask": child.QuoteAsk.String(),
			"venue_fee_rate": child.VenueFeeRate.String(), "effective_unit_price": child.EffectiveUnitPrice.String(),
			"execution_price": child.ExecutionPrice.String(), "gross_usd": child.GrossUSD.String(),
			"venue_fee_usd": child.VenueFeeUSD.String(), "status": child.Status, "failure_code": optionalString(child.FailureCode),
			"quote_observed_at": child.QuoteObservedAt.Format(timeFormat), "fills": fills,
		})
	}
	return map[string]any{
		"id": value.ID, "sequence": value.Sequence, "side": value.Side, "asset": value.Asset,
		"quote_currency": value.QuoteCurrency, "input_amount": value.InputAmount.String(),
		"filled_quantity": value.FilledQuantity.String(), "reference_price": value.ReferencePrice.String(),
		"average_execution_price": value.AverageExecutionPrice.String(), "gross_usd": value.GrossUSD.String(),
		"venue_fee_usd": value.VenueFeeUSD.String(), "platform_fee_usd": value.PlatformFeeUSD.String(),
		"final_customer_usd": value.FinalCustomerUSD.String(), "price_improvement_usd": value.PriceImprovementUSD.String(),
		"status": value.Status, "failure_code": optionalString(value.FailureCode),
		"ledger_transaction_id": optionalString(value.LedgerTransactionID), "children": children,
	}
}

func depositAddressDTO(value crypto.DepositAddress) map[string]any {
	return map[string]any{
		"id": value.ID, "asset": value.Asset, "network": value.Network, "provider": value.Provider,
		"external_address": value.ExternalAddress, "memo": optionalString(value.Memo), "status": value.Status,
		"created_at": value.CreatedAt.Format(timeFormat), "replayed": value.Replayed, "mode": "SIMULATED",
	}
}

func depositDTO(value crypto.Deposit) map[string]any {
	return map[string]any{
		"id": value.ID, "deposit_address_id": value.DepositAddressID, "asset": value.Asset,
		"network": value.Network, "quantity": value.Quantity.String(), "status": value.Status,
		"provider": value.Provider, "provider_transaction_id": value.ProviderTransactionID,
		"ledger_transaction_id": optionalString(value.LedgerTransactionID), "reason_code": optionalString(value.ReasonCode),
		"created_at": value.CreatedAt.Format(timeFormat), "replayed": value.Replayed, "mode": "SIMULATED",
	}
}

func withdrawalAddressDTO(value crypto.WithdrawalAddress) map[string]any {
	return map[string]any{
		"id": value.ID, "asset": value.Asset, "network": value.Network, "external_address": value.ExternalAddress,
		"label": value.Label, "status": value.Status, "risk_level": value.RiskLevel,
		"cooling_until": optionalTime(value.CoolingUntil), "cooling_reason": value.CoolingReason,
		"policy_version": value.PolicyVersion, "created_at": value.CreatedAt.Format(timeFormat),
		"updated_at": value.UpdatedAt.Format(timeFormat), "replayed": value.Replayed, "mode": "SIMULATED",
	}
}

func withdrawalDTO(value crypto.Withdrawal) map[string]any {
	return map[string]any{
		"id": value.ID, "withdrawal_address_id": value.WithdrawalAddressID, "asset": value.Asset,
		"network": value.Network, "quantity": value.Quantity.String(), "status": value.Status,
		"reason_code": value.ReasonCode, "next_action": value.NextAction, "provider": value.Provider,
		"provider_withdrawal_id": value.ProviderWithdrawalID, "created_at": value.CreatedAt.Format(timeFormat),
		"updated_at": value.UpdatedAt.Format(timeFormat), "replayed": value.Replayed, "mode": "SIMULATED",
	}
}

func reconciliationDTO(value crypto.ReconciliationRun) map[string]any {
	return map[string]any{
		"id": value.ID, "asset": value.Asset, "ledger_amount": value.LedgerAmount.String(),
		"custody_amount": value.CustodyAmount.String(), "difference": value.Difference.String(),
		"status": value.Status, "compliance_case_id": optionalString(value.ComplianceCase),
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
