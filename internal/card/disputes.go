package card

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/notification"
	"github.com/jackc/pgx/v5"
)

func (service *Service) OpenDispute(ctx context.Context, command OpenDisputeCommand) (Dispute, error) {
	command.ReasonCode = strings.ToUpper(strings.TrimSpace(command.ReasonCode))
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || command.CaptureID == "" ||
		!command.AmountUSD.IsPositive() || !reasonCodePattern.MatchString(command.ReasonCode) {
		return Dispute{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Dispute{}, err
	}
	captureID, err := parseUUID(command.CaptureID)
	if err != nil {
		return Dispute{}, ErrCaptureNotFound
	}
	requestHash, _ := hashValue(struct{ CaptureID, Amount, Reason string }{
		command.CaptureID, command.AmountUSD.String(), command.ReasonCode,
	})
	disputeID, err := newUUID()
	if err != nil {
		return Dispute{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Dispute{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil {
		return Dispute{}, err
	}
	_, err = queries.InsertCommandRequest(ctx, store.InsertCommandRequestParams{
		CustomerReference: profile.CustomerReference, Scope: "card.dispute.open",
		IdempotencyKey: command.IdempotencyKey, RequestHash: requestHash, ResourceID: disputeID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetCommandRequest(ctx, store.GetCommandRequestParams{
			CustomerReference: profile.CustomerReference, Scope: "card.dispute.open", IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil || existingRequest.RequestHash != requestHash {
			return Dispute{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetDispute(ctx, existingRequest.ResourceID)
		if getErr != nil {
			return Dispute{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Dispute{}, err
		}
		return disputeFromStore(existing), nil
	}
	if err != nil {
		return Dispute{}, err
	}
	capture, err := queries.GetCaptureForUpdate(ctx, captureID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Dispute{}, ErrCaptureNotFound
	}
	if err != nil {
		return Dispute{}, err
	}
	authorization, err := queries.GetAuthorizationForUpdate(ctx, capture.AuthorizationID)
	if err != nil || authorization.CustomerReference != profile.CustomerReference {
		return Dispute{}, ErrCaptureNotFound
	}
	reserved, err := queries.SumDisputedAmountForCapture(ctx, capture.ID)
	if err != nil {
		return Dispute{}, err
	}
	available, err := capture.SettledUsd.Sub(capture.RefundedUsd)
	if err != nil {
		return Dispute{}, err
	}
	available, err = available.Sub(reserved)
	if err != nil || command.AmountUSD.Compare(available) > 0 {
		return Dispute{}, ErrRefundExceeded
	}
	complianceCase, err := createComplianceCase(
		ctx, tx, profile.CustomerReference, "CARD_DISPUTE", "card.dispute", disputeID.String(),
		"CARD_TRANSACTION_DISPUTED", "Monitor the case center for updates or requests for supporting information.",
	)
	if err != nil {
		return Dispute{}, err
	}
	created, err := queries.CreateDispute(ctx, store.CreateDisputeParams{
		ID: disputeID, CaptureID: capture.ID, CustomerReference: profile.CustomerReference,
		AmountUsd: command.AmountUSD, ReasonCode: command.ReasonCode, ComplianceCaseID: complianceCase.ID,
	})
	if err != nil {
		return Dispute{}, err
	}
	payload, _ := json.Marshal(map[string]string{"dispute_id": disputeID.String(), "status": created.Status})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.dispute.opened", ResourceType: "card.dispute", ResourceID: disputeID.String(),
		ActorType: "USER", ActorID: profile.CustomerReference, CorrelationID: disputeID, Metadata: payload,
		AggregateType: "card.dispute", AggregateID: disputeID.String(), EventType: "card.dispute.opened", Payload: payload,
	}); err != nil {
		return Dispute{}, err
	}
	if _, err := service.notifications.Record(ctx, tx, notification.Event{
		NotificationEventID: "card.dispute.opened." + disputeID.String(), CustomerReference: profile.CustomerReference,
		Category: "CARD", EventType: "card.dispute.opened", TitleKey: "notification.card.dispute.opened.title",
		BodyKey: "notification.card.dispute.opened.body", ResourceType: "card.dispute", ResourceID: disputeID.String(), ActionPath: "/card/disputes",
	}); err != nil {
		return Dispute{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Dispute{}, err
	}
	return disputeFromStore(created), nil
}

func (service *Service) ResolveDispute(ctx context.Context, command ResolveDisputeCommand) (Dispute, error) {
	command.ReasonCode = strings.ToUpper(strings.TrimSpace(command.ReasonCode))
	if !authorizedDisputeAdmin(command.Actor) || command.DisputeID == "" || !reasonCodePattern.MatchString(command.ReasonCode) {
		return Dispute{}, ErrAdminUnauthorized
	}
	disputeID, err := parseUUID(command.DisputeID)
	if err != nil {
		return Dispute{}, ErrDisputeNotFound
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Dispute{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	dispute, err := queries.GetDisputeForUpdate(ctx, disputeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Dispute{}, ErrDisputeNotFound
	}
	if err != nil || dispute.Status == "RESOLVED" {
		return Dispute{}, ErrInvalidState
	}
	profile, err := queries.GetCustomerProfileForUpdate(ctx, dispute.CustomerReference)
	if err != nil {
		return Dispute{}, err
	}
	var ledgerTransactionID = uuidValue("")
	outcome, caseStatus, nextAction := "REJECTED", "REJECTED", "Review the decision and contact support if you have new information."
	if command.Accept {
		capture, getErr := queries.GetCaptureForUpdate(ctx, dispute.CaptureID)
		if getErr != nil {
			return Dispute{}, getErr
		}
		receivableReduction, cashCredit, splitErr := refundAllocation(ctx, queries, capture, dispute.AmountUsd)
		if splitErr != nil {
			return Dispute{}, splitErr
		}
		posting, postErr := service.postDisputeCredit(ctx, tx, profile, dispute.ID.String(), receivableReduction, cashCredit, command.Actor)
		if postErr != nil {
			return Dispute{}, postErr
		}
		ledgerTransactionID = uuidValue(posting.TransactionID)
		outcome, caseStatus = "ACCEPTED", "APPROVED"
		nextAction = "The accepted dispute credit is reflected in your card and cash balances."
	}
	updated, err := queries.ResolveDispute(ctx, store.ResolveDisputeParams{
		ID: dispute.ID, Outcome: textValue(outcome), ResolutionLedgerTransactionID: ledgerTransactionID,
	})
	if err != nil {
		return Dispute{}, err
	}
	if err := transitionComplianceCase(ctx, tx, dispute.ComplianceCaseID.String(), command.Actor, caseStatus, command.ReasonCode, nextAction); err != nil {
		return Dispute{}, err
	}
	payload, _ := json.Marshal(map[string]string{"dispute_id": dispute.ID.String(), "outcome": outcome, "reason_code": command.ReasonCode})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.dispute.resolved", ResourceType: "card.dispute", ResourceID: dispute.ID.String(),
		ActorType: "ADMIN", ActorID: command.Actor.ID, CorrelationID: dispute.ID, Metadata: payload,
		AggregateType: "card.dispute", AggregateID: dispute.ID.String(), EventType: "card.dispute.resolved", Payload: payload,
	}); err != nil {
		return Dispute{}, err
	}
	if _, err := service.notifications.Record(ctx, tx, notification.Event{
		NotificationEventID: "card.dispute.resolved." + dispute.ID.String(), CustomerReference: dispute.CustomerReference,
		Category: "CARD", EventType: "card.dispute.resolved", TitleKey: "notification.card.dispute.resolved.title",
		BodyKey: "notification.card.dispute.resolved.body", ResourceType: "card.dispute", ResourceID: dispute.ID.String(), ActionPath: "/card/disputes",
	}); err != nil {
		return Dispute{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Dispute{}, err
	}
	return disputeFromStore(updated), nil
}

func (service *Service) ListDisputes(ctx context.Context, accessToken string, pageSize int32) ([]Dispute, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	return service.listDisputes(ctx, profile.CustomerReference, pageSize)
}

func (service *Service) ListAdminDisputes(ctx context.Context, actor AdminActor, pageSize int32) ([]Dispute, error) {
	if !authorizedDisputeAdmin(actor) {
		return nil, ErrAdminUnauthorized
	}
	return service.listDisputes(ctx, "", pageSize)
}

func (service *Service) listDisputes(ctx context.Context, customerReference string, pageSize int32) ([]Dispute, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(service.database).ListDisputes(ctx, store.ListDisputesParams{CustomerFilter: customerReference, PageSize: pageSize})
	if err != nil {
		return nil, err
	}
	result := make([]Dispute, 0, len(rows))
	for _, row := range rows {
		result = append(result, disputeFromStore(row))
	}
	return result, nil
}

func disputeFromStore(value store.CardDispute) Dispute {
	var resolvedAt *time.Time
	if value.ResolvedAt.Valid {
		resolved := value.ResolvedAt.Time.UTC()
		resolvedAt = &resolved
	}
	return Dispute{
		ID: value.ID.String(), CaptureID: value.CaptureID.String(), CustomerReference: value.CustomerReference, AmountUSD: value.AmountUsd,
		ReasonCode: value.ReasonCode, Status: value.Status, Outcome: value.Outcome.String,
		ComplianceCaseID: value.ComplianceCaseID.String(), LedgerTransactionID: value.ResolutionLedgerTransactionID.String(),
		OpenedAt: value.OpenedAt.Time.UTC(), ResolvedAt: resolvedAt,
	}
}

func authorizedDisputeAdmin(actor AdminActor) bool {
	if actor.ID == "" {
		return false
	}
	switch actor.Role {
	case "OPERATIONS", "COMPLIANCE_ANALYST", "RISK_ANALYST", "ADMIN":
		return true
	default:
		return false
	}
}
