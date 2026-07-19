package card

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/notification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) ReverseAuthorization(ctx context.Context, command ReversalCommand) (Authorization, error) {
	if !service.localSimulationAllowed() || command.ExternalEventID == "" || command.AuthorizationID == "" || !command.AmountUSD.IsPositive() {
		return Authorization{}, ErrInvalidCommand
	}
	if command.OccurredAt.IsZero() {
		command.OccurredAt = service.now().UTC()
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Authorization{}, err
	}
	authorizationID, err := parseUUID(command.AuthorizationID)
	if err != nil {
		return Authorization{}, ErrAuthorizationNotFound
	}
	payloadHash, _ := hashValue(struct{ AuthorizationID, Amount, OccurredAt string }{
		command.AuthorizationID, command.AmountUSD.String(), command.OccurredAt.UTC().Format(time.RFC3339Nano),
	})
	reversalID, _ := newUUID()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Authorization{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	locked, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return Authorization{}, err
	}
	inserted, err := queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider: service.provider.Name(), ExternalEventID: command.ExternalEventID, EventType: "REVERSAL",
		PayloadHash: payloadHash, ResourceType: "card.authorization", ResourceID: authorizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingEvent, getErr := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: service.provider.Name(), ExternalEventID: command.ExternalEventID})
		if getErr != nil || existingEvent.PayloadHash != payloadHash || existingEvent.EventType != "REVERSAL" {
			return Authorization{}, ErrProviderEventConflict
		}
		existing, getErr := queries.GetCustomerAuthorization(ctx, store.GetCustomerAuthorizationParams{ID: authorizationID, CustomerReference: profile.CustomerReference})
		if getErr != nil {
			return Authorization{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Authorization{}, err
		}
		return authorizationFromStore(existing, true), nil
	}
	if err != nil || inserted != authorizationID {
		return Authorization{}, err
	}
	authorization, err := queries.GetAuthorizationForUpdate(ctx, authorizationID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && authorization.CustomerReference != profile.CustomerReference {
		return Authorization{}, ErrAuthorizationNotFound
	}
	if err != nil || authorization.Status == "DECLINED" || authorization.Status == "CAPTURED" || authorization.Status == "REVERSED" {
		return Authorization{}, ErrInvalidState
	}
	captures, err := queries.ListCapturesForAuthorization(ctx, authorization.ID)
	if err != nil {
		return Authorization{}, err
	}
	releasedByCapture := money.Zero()
	for _, capture := range captures {
		releasedByCapture, err = releasedByCapture.Add(capture.HoldReleasedUsd)
		if err != nil {
			return Authorization{}, err
		}
	}
	remaining, err := authorization.AuthorizedUsd.Sub(authorization.ReversedUsd)
	if err != nil {
		return Authorization{}, err
	}
	remaining, err = remaining.Sub(releasedByCapture)
	if err != nil || command.AmountUSD.Compare(remaining) > 0 {
		return Authorization{}, ErrInvalidCommand
	}
	posting, err := service.releaseAuthorizationHold(ctx, tx, locked, reversalID.String(), command.AmountUSD)
	if err != nil {
		return Authorization{}, err
	}
	if _, err := queries.CreateReversal(ctx, store.CreateReversalParams{
		ID: reversalID, AuthorizationID: authorization.ID, Provider: service.provider.Name(),
		ExternalReversalID: command.ExternalEventID, ReversedUsd: command.AmountUSD,
		LedgerTransactionID: uuidValue(posting.TransactionID), OccurredAt: timestamptz(command.OccurredAt),
	}); err != nil {
		return Authorization{}, err
	}
	newReversed, err := authorization.ReversedUsd.Add(command.AmountUSD)
	if err != nil {
		return Authorization{}, err
	}
	remainingAfter, _ := remaining.Sub(command.AmountUSD)
	status := authorization.Status
	if !remainingAfter.IsPositive() {
		status = "REVERSED"
	}
	updated, err := queries.UpdateAuthorizationTotals(ctx, store.UpdateAuthorizationTotalsParams{
		ID: authorization.ID, Status: status, CapturedUsd: authorization.CapturedUsd, ReversedUsd: newReversed,
	})
	if err != nil {
		return Authorization{}, err
	}
	payload, _ := json.Marshal(map[string]string{"authorization_id": authorization.ID.String(), "reversed_usd": command.AmountUSD.String()})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.authorization.reversed", ResourceType: "card.authorization", ResourceID: authorization.ID.String(),
		ActorType: "PROVIDER", ActorID: service.provider.Name(), CorrelationID: reversalID, Metadata: payload,
		AggregateType: "card.authorization", AggregateID: authorization.ID.String(), EventType: "card.authorization.reversed", Payload: payload,
	}); err != nil {
		return Authorization{}, err
	}
	if _, err := service.notifications.Record(ctx, tx, notification.Event{
		NotificationEventID: "card.reversal." + command.ExternalEventID, CustomerReference: profile.CustomerReference,
		Category: "CARD", EventType: "card.authorization.reversed", TitleKey: "notification.card.reversal.title",
		BodyKey: "notification.card.reversal.body", ResourceType: "card.authorization",
		ResourceID: authorization.ID.String(), ActionPath: "/card",
	}); err != nil {
		return Authorization{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Authorization{}, err
	}
	return authorizationFromStore(updated, false), nil
}

func (service *Service) Refund(ctx context.Context, command RefundCommand) (Refund, error) {
	if !service.localSimulationAllowed() || command.ExternalEventID == "" || command.CaptureID == "" || !command.AmountUSD.IsPositive() {
		return Refund{}, ErrInvalidCommand
	}
	if command.OccurredAt.IsZero() {
		command.OccurredAt = service.now().UTC()
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Refund{}, err
	}
	captureID, err := parseUUID(command.CaptureID)
	if err != nil {
		return Refund{}, ErrCaptureNotFound
	}
	payloadHash, _ := hashValue(struct{ CaptureID, Amount, OccurredAt string }{
		command.CaptureID, command.AmountUSD.String(), command.OccurredAt.UTC().Format(time.RFC3339Nano),
	})
	refundID, _ := newUUID()
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Refund{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	locked, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return Refund{}, err
	}
	inserted, err := queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider: service.provider.Name(), ExternalEventID: command.ExternalEventID, EventType: "REFUND",
		PayloadHash: payloadHash, ResourceType: "card.refund", ResourceID: refundID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingEvent, getErr := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: service.provider.Name(), ExternalEventID: command.ExternalEventID})
		if getErr != nil || existingEvent.PayloadHash != payloadHash || existingEvent.EventType != "REFUND" {
			return Refund{}, ErrProviderEventConflict
		}
		existing, getErr := queries.GetRefund(ctx, existingEvent.ResourceID)
		if getErr != nil {
			return Refund{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Refund{}, err
		}
		return refundFromStore(existing, true), nil
	}
	if err != nil || inserted != refundID {
		return Refund{}, err
	}
	result, err := service.applyRefund(ctx, tx, locked, captureID, refundID, command.ExternalEventID, command.AmountUSD, command.OccurredAt)
	if err != nil {
		return Refund{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Refund{}, err
	}
	return result, nil
}

func (service *Service) applyRefund(ctx context.Context, tx pgx.Tx, profile store.CardCustomerProfile, captureID, refundID pgtype.UUID, externalID string, amount money.Decimal, occurredAt time.Time) (Refund, error) {
	queries := store.New(tx)
	capture, err := queries.GetCaptureForUpdate(ctx, captureID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Refund{}, ErrCaptureNotFound
	}
	if err != nil {
		return Refund{}, err
	}
	authorization, err := queries.GetAuthorizationForUpdate(ctx, capture.AuthorizationID)
	if err != nil || authorization.CustomerReference != profile.CustomerReference {
		return Refund{}, ErrCaptureNotFound
	}
	remainingRefund, err := capture.SettledUsd.Sub(capture.RefundedUsd)
	if err != nil || amount.Compare(remainingRefund) > 0 {
		return Refund{}, ErrRefundExceeded
	}
	receivableReduction, cashCredit, err := refundAllocation(ctx, queries, capture, amount)
	if err != nil {
		return Refund{}, err
	}
	posting, err := service.postRefund(ctx, tx, profile, refundID.String(), receivableReduction, cashCredit)
	if err != nil {
		return Refund{}, err
	}
	created, err := queries.CreateRefund(ctx, store.CreateRefundParams{
		ID: refundID, CaptureID: capture.ID, Provider: service.provider.Name(), ExternalRefundID: externalID,
		RefundUsd: amount, ReceivableReductionUsd: receivableReduction, CashCreditUsd: cashCredit,
		LedgerTransactionID: uuidValue(posting.TransactionID), OccurredAt: timestamptz(occurredAt),
	})
	if err != nil {
		return Refund{}, err
	}
	newRefunded, err := capture.RefundedUsd.Add(amount)
	if err != nil {
		return Refund{}, err
	}
	status := "PARTIALLY_REFUNDED"
	if newRefunded.Equal(capture.SettledUsd) {
		status = "REFUNDED"
	}
	if _, err := queries.UpdateCaptureRefund(ctx, store.UpdateCaptureRefundParams{ID: capture.ID, RefundedUsd: newRefunded, Status: status}); err != nil {
		return Refund{}, err
	}
	payload, _ := json.Marshal(map[string]string{"refund_id": refundID.String(), "refund_usd": amount.String()})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.refund.posted", ResourceType: "card.refund", ResourceID: refundID.String(),
		ActorType: "PROVIDER", ActorID: service.provider.Name(), CorrelationID: refundID, Metadata: payload,
		AggregateType: "card.refund", AggregateID: refundID.String(), EventType: "card.refund.posted", Payload: payload,
	}); err != nil {
		return Refund{}, err
	}
	if _, err := service.notifications.Record(ctx, tx, notification.Event{
		NotificationEventID: "card.refund." + externalID, CustomerReference: profile.CustomerReference,
		Category: "CARD", EventType: "card.refund.posted", TitleKey: "notification.card.refund.title",
		BodyKey: "notification.card.refund.body", ResourceType: "card.refund", ResourceID: refundID.String(), ActionPath: "/card",
	}); err != nil {
		return Refund{}, err
	}
	return refundFromStore(created, false), nil
}

func refundAllocation(ctx context.Context, queries *store.Queries, capture store.CardCapture, amount money.Decimal) (money.Decimal, money.Decimal, error) {
	previousRefunds, err := queries.ListRefundsForCapture(ctx, capture.ID)
	if err != nil {
		return money.Decimal{}, money.Decimal{}, err
	}
	priorAdjustments := money.Zero()
	for _, previous := range previousRefunds {
		priorAdjustments, err = priorAdjustments.Add(previous.RefundUsd)
		if err != nil {
			return money.Decimal{}, money.Decimal{}, err
		}
	}
	acceptedDisputes, err := queries.SumAcceptedDisputeAmountForCapture(ctx, capture.ID)
	if err != nil {
		return money.Decimal{}, money.Decimal{}, err
	}
	priorAdjustments, err = priorAdjustments.Add(acceptedDisputes)
	if err != nil {
		return money.Decimal{}, money.Decimal{}, err
	}
	maximumAdjustment, err := capture.SettledUsd.Sub(priorAdjustments)
	if err != nil || amount.Compare(maximumAdjustment) > 0 {
		return money.Decimal{}, money.Decimal{}, ErrRefundExceeded
	}
	repaid, err := capture.CashRepaidUsd.Add(capture.AutoSellRepaidUsd)
	if err != nil {
		return money.Decimal{}, money.Decimal{}, err
	}
	openReceivable, err := capture.SettledUsd.Sub(repaid)
	if err != nil {
		return money.Decimal{}, money.Decimal{}, err
	}
	openReceivable, err = openReceivable.Sub(priorAdjustments)
	if err != nil {
		return money.Decimal{}, money.Decimal{}, err
	}
	receivableReduction := minimum(amount, positiveRemainder(openReceivable))
	cashCredit, err := amount.Sub(receivableReduction)
	if err != nil {
		return money.Decimal{}, money.Decimal{}, err
	}
	return receivableReduction, cashCredit, nil
}

func refundFromStore(value store.CardRefund, replayed bool) Refund {
	return Refund{
		ID: value.ID.String(), CaptureID: value.CaptureID.String(), ExternalRefundID: value.ExternalRefundID,
		RefundUSD: value.RefundUsd, ReceivableReductionUSD: value.ReceivableReductionUsd,
		CashCreditUSD: value.CashCreditUsd, LedgerTransactionID: value.LedgerTransactionID.String(),
		Replayed: replayed, OccurredAt: value.OccurredAt.Time.UTC(),
	}
}

func normalizeReason(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }
