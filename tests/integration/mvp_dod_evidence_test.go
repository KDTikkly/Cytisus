//go:build integration && evidence

package integration

import "testing"

// TestMVPDefinitionOfDoneEvidence is an explicit Prompt 8 evidence runner.
// Passing subtests execute the same real PostgreSQL-backed synthetic flows as
// the normal integration suite. Skipped subtests are intentional evidence of
// incomplete or externally blocked MVP Definition of Done requirements.
func TestMVPDefinitionOfDoneEvidence(t *testing.T) {
	t.Run("01_registration_and_passkey_login_PARTIAL", func(t *testing.T) {
		TestPaperAPIEndToEnd(t)
		t.Skip("synthetic fixture registration passes; passkey login is not implemented")
	})
	t.Run("02_paper_trading_PASS", TestPaperSecuritiesVerticalSlice)
	t.Run("03_kyc_and_investment_eligibility_FAIL", func(t *testing.T) {
		t.Skip("progressive KYC and investment-eligibility onboarding are not implemented")
	})
	t.Run("04_live_account_opening_BLOCKED", func(t *testing.T) {
		t.Skip("requires separately approved legal structure and licensed broker/clearing providers")
	})
	t.Run("05_ach_and_wire_funding_PASS", TestACHWireWithdrawalComplianceAndReconciliation)
	t.Run("06_stock_etf_crypto_orders_and_fills_PASS", func(t *testing.T) {
		TestPaperOrderConcurrentIdempotency(t)
		TestCryptoAPIEndToEnd(t)
	})
	t.Run("07_double_entry_posting_and_settlement_PASS", TestLedgerPostingIdempotencyProjectionAndConstraints)
	t.Run("08_crypto_deposit_conversion_withdrawal_review_PASS", TestCryptoRoutingCustodyReviewAndReconciliation)
	t.Run("09_card_auth_clearing_refund_auto_sell_PASS", func(t *testing.T) {
		TestCardAPIEndToEnd(t)
		TestCardProtectedAutoSellAndConcurrentReplay(t)
	})
	t.Run("10_rwa_mint_to_vault_PASS", TestRwaMintDividendBurnRecoveryAndInvariants)
	t.Run("11_external_permissioned_address_PASS", TestRwaExternalAddressProofCoolingAndExternalCustody)
	t.Run("12_rwa_transfer_and_redemption_PARTIAL", func(t *testing.T) {
		TestRwaExternalAddressProofCoolingAndExternalCustody(t)
		t.Skip("whitelist transfer passes at Solidity contract level, but no application/API transfer workflow exists")
	})
	t.Run("13_cash_dividend_to_usd_cash_PASS", TestRwaMintDividendBurnRecoveryAndInvariants)
	t.Run("14_closed_loop_bank_withdrawal_PASS", TestNewSameNameWithdrawalRequiresCoolingEnhancedReviewAndMakerChecker)
	t.Run("15_statement_export_FAIL", func(t *testing.T) {
		t.Skip("Card statement state exists, but MVP report/CSV/PDF export is not implemented")
	})
	t.Run("16_admin_case_review_and_maker_checker_PASS", TestNewSameNameWithdrawalRequiresCoolingEnhancedReviewAndMakerChecker)
	t.Run("17_account_recovery_FAIL", func(t *testing.T) {
		t.Skip("account recovery workflow is not implemented")
	})
	t.Run("18_read_only_security_mode_FAIL", func(t *testing.T) {
		t.Skip("Read-only Security Mode is not implemented")
	})
	t.Run("19_protective_sell_FAIL", func(t *testing.T) {
		t.Skip("protective sell authorization and execution workflow is not implemented")
	})
	t.Run("20_reconciliation_and_break_handling_PASS", func(t *testing.T) {
		TestThirdPartyWithdrawalProviderTimeoutConcurrencyAndDifference(t)
		TestCryptoRoutingCustodyReviewAndReconciliation(t)
		TestCardAPIEndToEnd(t)
		TestRwaMintDividendBurnRecoveryAndInvariants(t)
	})
}
