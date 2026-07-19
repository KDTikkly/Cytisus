package rwaapi

import (
	"time"

	"github.com/KDTikkly/Cytisus/internal/rwa"
)

func assetDTO(v rwa.Asset) map[string]any {
	return map[string]any{"id": v.ID, "instrument_id": v.InstrumentID, "underlying_symbol": v.UnderlyingSymbol, "token_name": v.TokenName, "token_symbol": v.TokenSymbol, "token_decimals": v.TokenDecimals, "contract_address": v.ContractAddress, "platform_vault_address": v.PlatformVaultAddress, "chain_name": v.ChainName, "chain_id": v.ChainID, "simulation_disclaimer": v.SimulationDisclaimer, "whole_settled_shares_only": true, "one_token_per_locked_share": true, "bridge_supported": false}
}
func holdingDTO(v rwa.Holding) map[string]any {
	return map[string]any{"asset_id": v.AssetID, "custody_mode": v.CustodyMode, "destination_address": v.DestinationAddress, "quantity": v.Quantity.String(), "updated_at": formatTime(v.UpdatedAt)}
}
func mintDTO(v rwa.Mint) map[string]any {
	return map[string]any{"id": v.ID, "asset_id": v.AssetID, "underlying_lock_id": v.UnderlyingLockID, "quantity": v.Quantity.String(), "custody_mode": v.CustodyMode, "destination_address": v.DestinationAddress, "operation_id": v.OperationID, "status": v.Status, "chain_transaction_hash": optional(v.ChainTransactionHash), "failure_code": optional(v.FailureCode), "created_at": formatTime(v.CreatedAt), "updated_at": formatTime(v.UpdatedAt), "simulation_disclaimer": rwa.SimulationOnly}
}
func redemptionDTO(v rwa.Redemption) map[string]any {
	return map[string]any{"id": v.ID, "asset_id": v.AssetID, "underlying_lock_id": v.UnderlyingLockID, "quantity": v.Quantity.String(), "source_address": v.SourceAddress, "operation_id": v.OperationID, "forced": v.Forced, "status": v.Status, "chain_transaction_hash": optional(v.ChainTransactionHash), "failure_code": optional(v.FailureCode), "completed_at": optionalTime(v.CompletedAt), "burn_before_share_release": true}
}
func addressDTO(v rwa.ExternalAddress) map[string]any {
	return map[string]any{"id": v.ID, "address": v.Address, "status": v.Status, "proof_reference": optional(v.ProofReference), "risk_reason_code": optional(v.RiskReasonCode), "cooling_ends_at": optionalTime(v.CoolingEndsAt), "activated_at": optionalTime(v.ActivatedAt), "suspended_at": optionalTime(v.SuspendedAt), "updated_at": formatTime(v.UpdatedAt)}
}
func reconciliationDTO(v rwa.Reconciliation) map[string]any {
	return map[string]any{"id": v.ID, "asset_id": v.AssetID, "chain_supply": v.ChainSupply.String(), "locked_shares": v.LockedShares.String(), "difference": v.Difference.String(), "status": v.Status, "compliance_case_id": optional(v.ComplianceCaseID), "observed_block": v.ObservedBlock, "automatic_adjustment": false, "completed_at": formatTime(v.CompletedAt)}
}
func dividendDTO(v rwa.Dividend) map[string]any {
	return map[string]any{"id": v.ID, "asset_id": v.AssetID, "external_reference": v.ExternalReference, "usd_per_share": v.USDPerShare.String(), "record_at": formatTime(v.RecordAt), "payable_at": formatTime(v.PayableAt), "status": v.Status, "settlement_currency": "USD", "drip": false, "updated_at": formatTime(v.UpdatedAt)}
}
func entitlementDTO(v rwa.DividendEntitlement) map[string]any {
	return map[string]any{"id": v.ID, "customer_reference": v.CustomerReference, "whole_shares": v.WholeShares.String(), "gross_usd": v.GrossUSD.String(), "withholding_usd": v.WithholdingUSD.String(), "net_usd": v.NetUSD.String(), "status": v.Status}
}
func formatTime(v time.Time) string { return v.UTC().Format(time.RFC3339Nano) }
func optional(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func optionalTime(v time.Time) any {
	if v.IsZero() {
		return nil
	}
	return formatTime(v)
}
