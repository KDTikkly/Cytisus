package card

import (
	"context"
	"fmt"

	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
)

func (service *Service) postAuthorizationHold(ctx context.Context, tx pgx.Tx, profile store.CardCustomerProfile, authorizationID string, amount money.Decimal) (ledger.PostingResult, error) {
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "card.authorization.hold", IdempotencyKey: "card.auth.hold." + authorizationID,
		TransactionType: "CARD_AUTHORIZATION_HOLD", PolicyVersion: profile.PolicyVersion,
		EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "PROVIDER", ID: service.provider.Name()},
		Entries: []ledger.Entry{
			{AccountID: profile.ReceivableLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: amount},
		},
	})
}

func (service *Service) postCaptureReceivable(ctx context.Context, tx pgx.Tx, profile store.CardCustomerProfile, captureID string, settledUSD, holdRelease money.Decimal) (ledger.PostingResult, error) {
	entries := []ledger.Entry{
		{AccountID: profile.ReceivableLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionReceivable, Direction: ledger.Debit, Amount: settledUSD},
		{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionReceivable, Direction: ledger.Credit, Amount: settledUSD},
	}
	if holdRelease.IsPositive() {
		entries = append(entries,
			ledger.Entry{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: holdRelease},
			ledger.Entry{AccountID: profile.ReceivableLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: holdRelease},
		)
	}
	posting, err := ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "card.capture.receivable", IdempotencyKey: "card.capture.receivable." + captureID,
		TransactionType: "CARD_CAPTURE_RECEIVABLE", PolicyVersion: profile.PolicyVersion,
		EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "PROVIDER", ID: service.provider.Name()}, Entries: entries,
	})
	if err != nil {
		return ledger.PostingResult{}, fmt.Errorf("post card capture receivable: %w", err)
	}
	return posting, nil
}

func (service *Service) releaseAuthorizationHold(ctx context.Context, tx pgx.Tx, profile store.CardCustomerProfile, scopeID string, amount money.Decimal) (ledger.PostingResult, error) {
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "card.authorization.release", IdempotencyKey: "card.auth.release." + scopeID,
		TransactionType: "CARD_AUTHORIZATION_RELEASE", PolicyVersion: profile.PolicyVersion,
		EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "PROVIDER", ID: service.provider.Name()},
		Entries: []ledger.Entry{
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: amount},
			{AccountID: profile.ReceivableLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: amount},
		},
	})
}

func (service *Service) postCashRepayment(ctx context.Context, tx pgx.Tx, profile store.CardCustomerProfile, captureID string, amount money.Decimal) (ledger.PostingResult, error) {
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "card.capture.repayment", IdempotencyKey: "card.capture.repayment." + captureID,
		TransactionType: "CARD_CAPTURE_REPAYMENT", PolicyVersion: profile.PolicyVersion,
		EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "SYSTEM", ID: "card-repayment"},
		Entries: []ledger.Entry{
			{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: amount},
			{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: amount},
			{AccountID: profile.ReceivableLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionReceivable, Direction: ledger.Credit, Amount: amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionReceivable, Direction: ledger.Debit, Amount: amount},
		},
	})
}

func (service *Service) postRefund(ctx context.Context, tx pgx.Tx, profile store.CardCustomerProfile, refundID string, receivableReduction, cashCredit money.Decimal) (ledger.PostingResult, error) {
	entries := refundEntries(profile, receivableReduction, cashCredit)
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "card.refund", IdempotencyKey: "card.refund." + refundID, TransactionType: "CARD_REFUND",
		PolicyVersion: profile.PolicyVersion, EffectiveAt: service.now().UTC(),
		Actor: ledger.Actor{Type: "PROVIDER", ID: service.provider.Name()}, Entries: entries,
	})
}

func (service *Service) postDisputeCredit(ctx context.Context, tx pgx.Tx, profile store.CardCustomerProfile, disputeID string, receivableReduction, cashCredit money.Decimal, actor AdminActor) (ledger.PostingResult, error) {
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "card.dispute.credit", IdempotencyKey: "card.dispute.credit." + disputeID,
		TransactionType: "CARD_DISPUTE_CREDIT", PolicyVersion: profile.PolicyVersion,
		EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "ADMIN", ID: actor.ID},
		Entries: refundEntries(profile, receivableReduction, cashCredit),
	})
}

func (service *Service) postStatementRepayment(ctx context.Context, tx pgx.Tx, profile store.CardCustomerProfile, statementID string, amount money.Decimal) (ledger.PostingResult, error) {
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "card.statement.repayment", IdempotencyKey: "card.statement.repayment." + statementID,
		TransactionType: "CARD_STATEMENT_REPAYMENT", PolicyVersion: profile.PolicyVersion,
		EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "USER", ID: profile.CustomerReference},
		Entries: []ledger.Entry{
			{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: amount},
			{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: amount},
			{AccountID: profile.ReceivableLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionReceivable, Direction: ledger.Credit, Amount: amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionReceivable, Direction: ledger.Debit, Amount: amount},
		},
	})
}

func refundEntries(profile store.CardCustomerProfile, receivableReduction, cashCredit money.Decimal) []ledger.Entry {
	entries := make([]ledger.Entry, 0, 6)
	if receivableReduction.IsPositive() {
		entries = append(entries,
			ledger.Entry{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionReceivable, Direction: ledger.Debit, Amount: receivableReduction},
			ledger.Entry{AccountID: profile.ReceivableLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionReceivable, Direction: ledger.Credit, Amount: receivableReduction},
		)
	}
	if cashCredit.IsPositive() {
		entries = append(entries,
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: cashCredit},
			ledger.Entry{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: cashCredit},
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: cashCredit},
			ledger.Entry{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: cashCredit},
		)
	}
	return entries
}
