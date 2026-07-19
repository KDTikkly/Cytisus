package card

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
)

type autoSellResult struct {
	Proceeds   money.Decimal
	Executions []AutoSellExecution
}

func (service *Service) executeAutoSell(
	ctx context.Context,
	tx pgx.Tx,
	profile store.CardCustomerProfile,
	captureID string,
	neededUSD money.Decimal,
	scenario cardprovider.Scenario,
) (autoSellResult, error) {
	queries := store.New(tx)
	mandate, err := queries.GetAutoSellMandateForUpdate(ctx, profile.CustomerReference)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (!mandate.Enabled || mandate.ValidUntil.Time.Before(service.now().UTC())) {
		return autoSellResult{}, ErrMandateRequired
	}
	if err != nil {
		return autoSellResult{}, err
	}
	assets, err := queries.ListAutoSellMandateAssets(ctx, mandate.ID)
	if err != nil {
		return autoSellResult{}, err
	}
	policy, err := queries.GetPolicy(ctx, profile.PolicyVersion)
	if err != nil {
		return autoSellResult{}, err
	}
	startOfDay := service.now().UTC().Truncate(24 * time.Hour)
	dailyUsed, err := queries.SumDailyAutoSell(ctx, store.SumDailyAutoSellParams{
		CustomerReference: profile.CustomerReference, SinceTime: timestamptz(startOfDay),
	})
	if err != nil {
		return autoSellResult{}, err
	}
	dailyRemaining, err := mandate.DailyMaxUsd.Sub(dailyUsed)
	if err != nil {
		return autoSellResult{}, err
	}
	remaining := minimum(neededUSD, positiveRemainder(dailyRemaining))
	result := autoSellResult{Executions: make([]AutoSellExecution, 0, len(assets))}
	for index, mandateAsset := range assets {
		if !remaining.IsPositive() {
			break
		}
		asset, assetErr := queries.GetCollateralAsset(ctx, mandateAsset.Symbol)
		account, accountErr := queries.GetCollateralAccount(ctx, store.GetCollateralAccountParams{
			CustomerReference: profile.CustomerReference, Symbol: mandateAsset.Symbol,
		})
		failureCode := ""
		if assetErr != nil || accountErr != nil {
			failureCode = "ASSET_UNAVAILABLE"
		} else if account.Frozen || account.Transferring {
			failureCode = "ASSET_RESTRICTED"
		} else if asset.QuoteStatus != "SIMULATED" || asset.MarketStatus != "OPEN" ||
			service.now().UTC().Sub(asset.ObservedAt.Time.UTC()) > time.Duration(policy.QuoteMaxAgeSeconds)*time.Second {
			failureCode = "REFERENCE_PRICE_UNAVAILABLE"
		}
		sequence := int16(index + 1)
		executionID, _ := newUUID()
		requested := money.MustParse("0.000000000000000001")
		protectedLimit := money.MustParse("0.000000000000000001")
		if failureCode == "" {
			ledgerService := ledger.NewService(tx)
			quantity, balanceErr := ledgerService.Balance(ctx, account.CustomerLedgerAccountID.String(), money.Currency(asset.Symbol), ledger.DimensionSettled)
			if balanceErr != nil {
				return autoSellResult{}, balanceErr
			}
			availableQuantity, subtractErr := quantity.Sub(mandateAsset.MinimumRetainQuantity)
			if subtractErr != nil {
				return autoSellResult{}, subtractErr
			}
			availableQuantity = positiveRemainder(availableQuantity)
			oneMinusGuardrail, subtractErr := money.MustParse("1").Sub(policy.ProtectedLimitGuardrailRate)
			if subtractErr != nil {
				return autoSellResult{}, subtractErr
			}
			protectedLimit, err = asset.ReferencePriceUsd.Multiply(oneMinusGuardrail, amountPolicy)
			if err != nil {
				return autoSellResult{}, err
			}
			// Size against the protected limit, not the reference quote. This guarantees
			// that an execution at the worst permitted price can still cover the
			// requested USD amount while preserving all price improvement as user cash.
			requested, err = remaining.Divide(protectedLimit, money.RoundingPolicy{Version: PolicyVersion, DecimalPlaces: 18, Mode: money.RoundAwayFromZero})
			if err != nil {
				return autoSellResult{}, err
			}
			requested = minimum(requested, availableQuantity)
			if !mandate.AllowFractional {
				requested, err = requested.Round(money.RoundingPolicy{Version: PolicyVersion, DecimalPlaces: 0, Mode: money.RoundTowardZero})
				if err != nil {
					return autoSellResult{}, err
				}
			}
			if !requested.IsPositive() {
				failureCode = "INSUFFICIENT_AUTHORIZED_QUANTITY"
			}
		}
		if failureCode != "" {
			stored, insertErr := queries.InsertAutoSellExecution(ctx, store.InsertAutoSellExecutionParams{
				ID: executionID, CaptureID: uuidValue(captureID), MandateID: mandate.ID, ExecutionSequence: sequence,
				Symbol: mandateAsset.Symbol, RequestedQuantity: requested, ProtectedLimitPrice: protectedLimit,
				Status: "FAILED", FailureCode: textValue(failureCode),
			})
			if insertErr != nil {
				return autoSellResult{}, insertErr
			}
			result.Executions = append(result.Executions, autoSellExecutionFromStore(stored))
			continue
		}
		providerCtx, cancel := service.withProviderTimeout(ctx)
		providerResult, providerErr := service.provider.ExecuteProtectedSell(providerCtx, cardprovider.ProtectedSellRequest{
			ClientOrderReference: fmt.Sprintf("capture.%s.%d", captureID, sequence), Symbol: mandateAsset.Symbol,
			RequestedQuantity: requested, ReferencePrice: asset.ReferencePriceUsd,
			ProtectedLimitPrice: protectedLimit, Scenario: scenario,
		})
		cancel()
		if providerErr != nil {
			switch {
			case errors.Is(providerErr, cardprovider.ErrTimeout):
				failureCode = "PROVIDER_TIMEOUT"
			case errors.Is(providerErr, cardprovider.ErrStaleQuote):
				failureCode = "REFERENCE_PRICE_UNAVAILABLE"
			default:
				failureCode = "PROVIDER_UNAVAILABLE"
			}
			providerResult.Status = "FAILED"
			providerResult.FailureCode = failureCode
		}
		if providerResult.Status == "FAILED" || !providerResult.FilledQuantity.IsPositive() {
			stored, insertErr := queries.InsertAutoSellExecution(ctx, store.InsertAutoSellExecutionParams{
				ID: executionID, CaptureID: uuidValue(captureID), MandateID: mandate.ID, ExecutionSequence: sequence,
				Symbol: mandateAsset.Symbol, RequestedQuantity: requested, ProtectedLimitPrice: protectedLimit,
				Status: "FAILED", FailureCode: textValue(providerResult.FailureCode),
			})
			if insertErr != nil {
				return autoSellResult{}, insertErr
			}
			result.Executions = append(result.Executions, autoSellExecutionFromStore(stored))
			continue
		}
		if providerResult.ExecutionPrice.Compare(protectedLimit) < 0 {
			return autoSellResult{}, fmt.Errorf("protected execution below limit: %w", ErrProtectedSellFailed)
		}
		account, err := queries.GetCollateralAccount(ctx, store.GetCollateralAccountParams{
			CustomerReference: profile.CustomerReference, Symbol: mandateAsset.Symbol,
		})
		if err != nil {
			return autoSellResult{}, err
		}
		posting, err := ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
			Scope: "card.auto-sell", IdempotencyKey: fmt.Sprintf("card.auto-sell.%s.%d", captureID, sequence),
			TransactionType: "CARD_PROTECTED_AUTO_SELL", PolicyVersion: profile.PolicyVersion,
			EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "SYSTEM", ID: "card-auto-sell"},
			Entries: []ledger.Entry{
				{AccountID: account.CustomerLedgerAccountID.String(), Currency: money.Currency(mandateAsset.Symbol), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: providerResult.FilledQuantity},
				{AccountID: account.ProviderInventoryLedgerAccountID.String(), Currency: money.Currency(mandateAsset.Symbol), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: providerResult.FilledQuantity},
				{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: providerResult.ProceedsUSD},
				{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: providerResult.ProceedsUSD},
				{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: providerResult.ProceedsUSD},
				{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: providerResult.ProceedsUSD},
			},
		})
		if err != nil {
			return autoSellResult{}, err
		}
		stored, err := queries.InsertAutoSellExecution(ctx, store.InsertAutoSellExecutionParams{
			ID: executionID, CaptureID: uuidValue(captureID), MandateID: mandate.ID, ExecutionSequence: sequence,
			Symbol: mandateAsset.Symbol, RequestedQuantity: requested, ProtectedLimitPrice: protectedLimit,
			ExecutionPrice: providerResult.ExecutionPrice, FilledQuantity: providerResult.FilledQuantity,
			ProceedsUsd: providerResult.ProceedsUSD, Status: providerResult.Status,
			LedgerTransactionID: uuidValue(posting.TransactionID),
		})
		if err != nil {
			return autoSellResult{}, err
		}
		result.Executions = append(result.Executions, autoSellExecutionFromStore(stored))
		result.Proceeds, err = result.Proceeds.Add(providerResult.ProceedsUSD)
		if err != nil {
			return autoSellResult{}, err
		}
		remaining, err = remaining.Sub(providerResult.ProceedsUSD)
		if err != nil {
			return autoSellResult{}, err
		}
		remaining = positiveRemainder(remaining)
	}
	return result, nil
}

func autoSellExecutionFromStore(value store.CardAutoSellExecution) AutoSellExecution {
	return AutoSellExecution{
		ID: value.ID.String(), Sequence: value.ExecutionSequence, Symbol: value.Symbol,
		RequestedQuantity: value.RequestedQuantity, ProtectedLimitPrice: value.ProtectedLimitPrice,
		ExecutionPrice: value.ExecutionPrice, FilledQuantity: value.FilledQuantity, ProceedsUSD: value.ProceedsUsd,
		Status: value.Status, FailureCode: value.FailureCode.String, LedgerTransactionID: value.LedgerTransactionID.String(),
	}
}

func autoSellFailureSummary(executions []AutoSellExecution) string {
	for _, execution := range executions {
		if execution.Status == "FAILED" && execution.FailureCode != "" {
			return execution.FailureCode
		}
	}
	return strings.ToUpper("insufficient protected liquidity")
}
