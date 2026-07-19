package card

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/notification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) Capture(ctx context.Context, command CaptureCommand) (Capture, error) {
	if !service.localSimulationAllowed() {
		return Capture{}, ErrSimulatorDisabled
	}
	command.MerchantCurrency = strings.ToUpper(strings.TrimSpace(command.MerchantCurrency))
	if command.ExternalEventID == "" || command.AuthorizationID == "" || !command.MerchantAmount.IsPositive() || len(command.MerchantCurrency) != 3 {
		return Capture{}, ErrInvalidCommand
	}
	if command.OccurredAt.IsZero() {
		command.OccurredAt = service.now().UTC()
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Capture{}, err
	}
	authorizationID, err := parseUUID(command.AuthorizationID)
	if err != nil {
		return Capture{}, ErrAuthorizationNotFound
	}
	payloadHash, _ := hashValue(struct {
		AuthorizationID, Amount, Currency, OccurredAt string
		Final                                         bool
		Scenario                                      cardprovider.Scenario
	}{command.AuthorizationID, command.MerchantAmount.String(), command.MerchantCurrency, command.OccurredAt.UTC().Format(time.RFC3339Nano), command.Final, command.SimulationScenario})
	captureID, err := newUUID()
	if err != nil {
		return Capture{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Capture{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	locked, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return Capture{}, err
	}
	inserted, err := queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider: service.provider.Name(), ExternalEventID: command.ExternalEventID, EventType: "CAPTURE",
		PayloadHash: payloadHash, ResourceType: "card.capture", ResourceID: captureID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingEvent, getErr := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: service.provider.Name(), ExternalEventID: command.ExternalEventID})
		if getErr != nil || existingEvent.PayloadHash != payloadHash || existingEvent.EventType != "CAPTURE" {
			return Capture{}, ErrProviderEventConflict
		}
		existing, getErr := service.captureWith(ctx, tx, existingEvent.ResourceID, true)
		if getErr != nil {
			return Capture{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Capture{}, err
		}
		return existing, nil
	}
	if err != nil || inserted != captureID {
		return Capture{}, err
	}
	authorization, err := queries.GetAuthorizationForUpdate(ctx, authorizationID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && authorization.CustomerReference != profile.CustomerReference {
		return Capture{}, ErrAuthorizationNotFound
	}
	if err != nil {
		return Capture{}, err
	}
	if authorization.Status == "DECLINED" || authorization.Status == "REVERSED" || authorization.Status == "CAPTURED" || authorization.MerchantCurrency != command.MerchantCurrency {
		return Capture{}, ErrInvalidState
	}
	policy, err := queries.GetPolicy(ctx, locked.PolicyVersion)
	if err != nil {
		return Capture{}, err
	}
	existingCaptures, err := queries.ListCapturesForAuthorization(ctx, authorization.ID)
	if err != nil {
		return Capture{}, err
	}
	totalMerchant := command.MerchantAmount
	previousMerchant := money.Zero()
	previousHoldRelease := money.Zero()
	for _, existing := range existingCaptures {
		previousMerchant, err = previousMerchant.Add(existing.MerchantAmount)
		if err != nil {
			return Capture{}, err
		}
		totalMerchant, err = totalMerchant.Add(existing.MerchantAmount)
		if err != nil {
			return Capture{}, err
		}
		previousHoldRelease, err = previousHoldRelease.Add(existing.HoldReleasedUsd)
		if err != nil {
			return Capture{}, err
		}
	}
	maximumMultiplier, _ := money.MustParse("1").Add(policy.MaximumTipRate)
	maximumMerchant, err := authorization.MerchantAmount.Multiply(maximumMultiplier, amountPolicy)
	if err != nil || totalMerchant.Compare(maximumMerchant) > 0 {
		return Capture{}, ErrTipExceeded
	}
	providerCtx, cancel := service.withProviderTimeout(ctx)
	defer cancel()
	fx, err := service.provider.FX(providerCtx, command.MerchantCurrency, command.SimulationScenario)
	if err != nil || fx.Status != "SIMULATED" {
		return Capture{}, ErrFXUnavailable
	}
	markupMultiplier, _ := money.MustParse("1").Add(policy.AuthFxMarkupRate)
	baseSettlement, err := command.MerchantAmount.Multiply(fx.USDPerUnit, amountPolicy)
	if err != nil {
		return Capture{}, err
	}
	settledUSD, err := baseSettlement.Multiply(markupMultiplier, amountPolicy)
	if err != nil {
		return Capture{}, err
	}
	tipMerchant, err := totalMerchant.Sub(authorization.MerchantAmount)
	if err != nil {
		return Capture{}, err
	}
	tipMerchant = positiveRemainder(tipMerchant)
	previousTipMerchant, err := previousMerchant.Sub(authorization.MerchantAmount)
	if err != nil {
		return Capture{}, err
	}
	previousTipMerchant = positiveRemainder(previousTipMerchant)
	tipMerchant, err = tipMerchant.Sub(previousTipMerchant)
	if err != nil {
		return Capture{}, err
	}
	tipMerchant = positiveRemainder(tipMerchant)
	tipUSD, err := tipMerchant.Multiply(fx.USDPerUnit, amountPolicy)
	if err != nil {
		return Capture{}, err
	}
	tipUSD, err = tipUSD.Multiply(markupMultiplier, amountPolicy)
	if err != nil {
		return Capture{}, err
	}
	remainingHold, err := authorization.AuthorizedUsd.Sub(authorization.ReversedUsd)
	if err != nil {
		return Capture{}, err
	}
	remainingHold, err = remainingHold.Sub(previousHoldRelease)
	if err != nil {
		return Capture{}, err
	}
	remainingHold = positiveRemainder(remainingHold)
	effectiveFinal := command.Final || totalMerchant.Compare(authorization.MerchantAmount) >= 0
	holdRelease := minimum(remainingHold, settledUSD)
	extraRelease := money.Zero()
	if effectiveFinal {
		holdRelease = remainingHold
		capturedPortion := minimum(remainingHold, settledUSD)
		extraRelease, err = holdRelease.Sub(capturedPortion)
		if err != nil {
			return Capture{}, err
		}
	}
	receivablePosting, err := service.postCaptureReceivable(ctx, tx, locked, captureID.String(), settledUSD, holdRelease)
	if err != nil {
		return Capture{}, err
	}
	cashRepaid, autoSellRepaid := money.Zero(), money.Zero()
	repaymentID := pgtype.UUID{}
	autoSellExecutions := []AutoSellExecution{}
	if locked.RepaymentMode != string(RepaymentMonthlyStatement) {
		ledgerService := ledger.NewService(tx)
		settledCash, balanceErr := ledgerService.Balance(ctx, locked.CashLedgerAccountID.String(), "USD", ledger.DimensionSettled)
		if balanceErr != nil {
			return Capture{}, balanceErr
		}
		withdrawableCash, balanceErr := ledgerService.Balance(ctx, locked.CashLedgerAccountID.String(), "USD", ledger.DimensionWithdrawable)
		if balanceErr != nil {
			return Capture{}, balanceErr
		}
		cashRepaid = minimum(settledUSD, minimum(positiveRemainder(settledCash), positiveRemainder(withdrawableCash)))
		shortfall, subtractErr := settledUSD.Sub(cashRepaid)
		if subtractErr != nil {
			return Capture{}, subtractErr
		}
		if shortfall.IsPositive() && locked.RepaymentMode == string(RepaymentCashThenAutoSell) {
			autoSell, autoSellErr := service.executeAutoSell(ctx, tx, locked, captureID.String(), shortfall, command.SimulationScenario)
			if autoSellErr != nil && !errors.Is(autoSellErr, ErrMandateRequired) {
				return Capture{}, autoSellErr
			}
			autoSellExecutions = autoSell.Executions
			autoSellRepaid = minimum(autoSell.Proceeds, shortfall)
		}
		totalRepaid, addErr := cashRepaid.Add(autoSellRepaid)
		if addErr != nil {
			return Capture{}, addErr
		}
		if totalRepaid.IsPositive() {
			posting, postErr := service.postCashRepayment(ctx, tx, locked, captureID.String(), totalRepaid)
			if postErr != nil {
				return Capture{}, postErr
			}
			repaymentID = uuidValue(posting.TransactionID)
		}
		remaining, subtractErr := settledUSD.Sub(totalRepaid)
		if subtractErr != nil {
			return Capture{}, subtractErr
		}
		if remaining.IsPositive() {
			if _, freezeErr := queries.FreezeSpending(ctx, store.FreezeSpendingParams{
				CustomerReference: locked.CustomerReference, ReasonCode: textValue("REPAYMENT_SHORTFALL"),
			}); freezeErr != nil {
				return Capture{}, freezeErr
			}
		}
	}
	created, err := queries.CreateCapture(ctx, store.CreateCaptureParams{
		ID: captureID, AuthorizationID: authorization.ID, Provider: service.provider.Name(),
		ExternalCaptureID: command.ExternalEventID, MerchantAmount: command.MerchantAmount,
		MerchantCurrency: command.MerchantCurrency, ClearingFxRate: fx.USDPerUnit,
		FxMarkupRate: policy.AuthFxMarkupRate, SettledUsd: settledUSD, TipUsd: tipUSD,
		HoldReleasedUsd: holdRelease, CashRepaidUsd: cashRepaid, AutoSellRepaidUsd: autoSellRepaid,
		ReceivableLedgerTransactionID: uuidValue(receivablePosting.TransactionID),
		RepaymentLedgerTransactionID:  repaymentID, OccurredAt: timestamptz(command.OccurredAt),
	})
	if err != nil {
		return Capture{}, fmt.Errorf("create card capture: %w", err)
	}
	newCaptured, err := authorization.CapturedUsd.Add(settledUSD)
	if err != nil {
		return Capture{}, err
	}
	newReversed, err := authorization.ReversedUsd.Add(extraRelease)
	if err != nil {
		return Capture{}, err
	}
	status := "PARTIALLY_CAPTURED"
	if effectiveFinal {
		status = "CAPTURED"
	}
	if _, err := queries.UpdateAuthorizationTotals(ctx, store.UpdateAuthorizationTotalsParams{
		ID: authorization.ID, Status: status, CapturedUsd: newCaptured, ReversedUsd: newReversed,
	}); err != nil {
		return Capture{}, err
	}
	payload, _ := json.Marshal(map[string]string{"capture_id": captureID.String(), "settled_usd": settledUSD.String(), "status": created.Status})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.capture.posted", ResourceType: "card.capture", ResourceID: captureID.String(),
		ActorType: "PROVIDER", ActorID: service.provider.Name(), CorrelationID: captureID, Metadata: payload,
		AggregateType: "card.capture", AggregateID: captureID.String(), EventType: "card.capture.posted", Payload: payload,
	}); err != nil {
		return Capture{}, err
	}
	if _, err := service.notifications.Record(ctx, tx, notification.Event{
		NotificationEventID: "card.capture." + command.ExternalEventID,
		CustomerReference:   profile.CustomerReference, Category: "CARD", EventType: "card.capture.posted",
		TitleKey: "notification.card.capture.title", BodyKey: "notification.card.capture.body",
		ResourceType: "card.capture", ResourceID: captureID.String(), ActionPath: "/card",
	}); err != nil {
		return Capture{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Capture{}, err
	}
	result := captureFromStore(created, false)
	result.AutoSellExecutions = autoSellExecutions
	return result, nil
}

func (service *Service) ListCaptures(ctx context.Context, accessToken string, pageSize int32) ([]Capture, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(service.database).ListCustomerCaptures(ctx, store.ListCustomerCapturesParams{CustomerReference: profile.CustomerReference, PageSize: pageSize})
	if err != nil {
		return nil, err
	}
	result := make([]Capture, 0, len(rows))
	for _, row := range rows {
		value, getErr := service.captureWith(ctx, service.database, row.ID, false)
		if getErr != nil {
			return nil, getErr
		}
		result = append(result, value)
	}
	return result, nil
}

func (service *Service) captureWith(ctx context.Context, database store.DBTX, id pgtype.UUID, replayed bool) (Capture, error) {
	queries := store.New(database)
	row, err := queries.GetCapture(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Capture{}, ErrCaptureNotFound
	}
	if err != nil {
		return Capture{}, err
	}
	executionRows, err := queries.ListAutoSellExecutions(ctx, row.ID)
	if err != nil {
		return Capture{}, err
	}
	value := captureFromStore(row, replayed)
	value.AutoSellExecutions = make([]AutoSellExecution, 0, len(executionRows))
	for _, execution := range executionRows {
		value.AutoSellExecutions = append(value.AutoSellExecutions, autoSellExecutionFromStore(execution))
	}
	return value, nil
}

func captureFromStore(value store.CardCapture, replayed bool) Capture {
	return Capture{
		ID: value.ID.String(), AuthorizationID: value.AuthorizationID.String(), ExternalCaptureID: value.ExternalCaptureID,
		MerchantAmount: value.MerchantAmount, MerchantCurrency: value.MerchantCurrency,
		ClearingFXRate: value.ClearingFxRate, FXMarkupRate: value.FxMarkupRate, SettledUSD: value.SettledUsd,
		TipUSD: value.TipUsd, HoldReleasedUSD: value.HoldReleasedUsd, CashRepaidUSD: value.CashRepaidUsd,
		AutoSellRepaidUSD: value.AutoSellRepaidUsd, RefundedUSD: value.RefundedUsd, Status: value.Status,
		ReceivableLedgerTransactionID: value.ReceivableLedgerTransactionID.String(),
		RepaymentLedgerTransactionID:  value.RepaymentLedgerTransactionID.String(), Replayed: replayed,
		OccurredAt: value.OccurredAt.Time.UTC(), CreatedAt: value.CreatedAt.Time.UTC(),
	}
}
