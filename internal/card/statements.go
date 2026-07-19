package card

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/notification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const statementPaymentWindow = 21 * 24 * time.Hour

func (service *Service) GenerateStatement(ctx context.Context, command GenerateStatementCommand) (Statement, error) {
	if !authorizedStatementAdmin(command.Actor) || command.CustomerReference == "" ||
		command.PeriodStart.IsZero() || command.PeriodEnd.IsZero() {
		return Statement{}, ErrAdminUnauthorized
	}
	periodStart := utcDate(command.PeriodStart)
	periodEnd := utcDate(command.PeriodEnd)
	if periodEnd.Before(periodStart) || periodEnd.After(utcDate(service.now())) {
		return Statement{}, ErrInvalidCommand
	}
	statementID, err := newUUID()
	if err != nil {
		return Statement{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Statement{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	profile, err := queries.GetCustomerProfileForUpdate(ctx, command.CustomerReference)
	if err != nil {
		return Statement{}, ErrInvalidCommand
	}
	if profile.RepaymentMode != string(RepaymentMonthlyStatement) {
		return Statement{}, ErrInvalidState
	}
	receivable, err := ledger.NewService(tx).Balance(ctx, profile.ReceivableLedgerAccountID.String(), "USD", ledger.DimensionReceivable)
	if err != nil {
		return Statement{}, err
	}
	dueAt := periodEnd.Add(24*time.Hour + statementPaymentWindow)
	created, err := queries.CreateStatement(ctx, store.CreateStatementParams{
		ID: statementID, CustomerReference: profile.CustomerReference,
		PeriodStart: dateValue(periodStart), PeriodEnd: dateValue(periodEnd),
		AmountDueUsd: positiveRemainder(receivable), DueAt: timestamptz(dueAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		created, err = queries.GetStatementByPeriod(ctx, store.GetStatementByPeriodParams{
			CustomerReference: profile.CustomerReference, PeriodStart: dateValue(periodStart), PeriodEnd: dateValue(periodEnd),
		})
	}
	if err != nil {
		return Statement{}, err
	}
	payload, _ := json.Marshal(map[string]string{"statement_id": created.ID.String(), "amount_due_usd": created.AmountDueUsd.String()})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.statement.generated", ResourceType: "card.statement", ResourceID: created.ID.String(),
		ActorType: "ADMIN", ActorID: command.Actor.ID, CorrelationID: created.ID, Metadata: payload,
		AggregateType: "card.statement", AggregateID: created.ID.String(), EventType: "card.statement.generated", Payload: payload,
	}); err != nil {
		return Statement{}, err
	}
	if _, err := service.notifications.Record(ctx, tx, notification.Event{
		NotificationEventID: "card.statement.generated." + created.ID.String(), CustomerReference: profile.CustomerReference,
		Category: "CARD", EventType: "card.statement.generated", TitleKey: "notification.card.statement.title",
		BodyKey: "notification.card.statement.body", ResourceType: "card.statement", ResourceID: created.ID.String(), ActionPath: "/card/statements",
	}); err != nil {
		return Statement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Statement{}, err
	}
	return statementFromStore(created), nil
}

func (service *Service) PayStatement(ctx context.Context, command PayStatementCommand) (Statement, error) {
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || command.StatementID == "" {
		return Statement{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Statement{}, err
	}
	statementID, err := parseUUID(command.StatementID)
	if err != nil {
		return Statement{}, ErrInvalidCommand
	}
	requestHash, _ := hashValue(struct{ StatementID string }{command.StatementID})
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Statement{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	locked, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return Statement{}, err
	}
	_, err = queries.InsertCommandRequest(ctx, store.InsertCommandRequestParams{
		CustomerReference: profile.CustomerReference, Scope: "card.statement.pay", IdempotencyKey: command.IdempotencyKey,
		RequestHash: requestHash, ResourceID: statementID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetCommandRequest(ctx, store.GetCommandRequestParams{
			CustomerReference: profile.CustomerReference, Scope: "card.statement.pay", IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil || existingRequest.RequestHash != requestHash {
			return Statement{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetStatement(ctx, statementID)
		if getErr != nil || existing.CustomerReference != profile.CustomerReference {
			return Statement{}, ErrInvalidCommand
		}
		if err := tx.Commit(ctx); err != nil {
			return Statement{}, err
		}
		return statementFromStore(existing), nil
	}
	if err != nil {
		return Statement{}, err
	}
	statement, err := queries.GetStatementForUpdate(ctx, statementID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && statement.CustomerReference != profile.CustomerReference {
		return Statement{}, ErrInvalidCommand
	}
	if err != nil || statement.Status == "PAID" || !statement.AmountDueUsd.IsPositive() {
		return Statement{}, ErrInvalidState
	}
	ledgerService := ledger.NewService(tx)
	settled, err := ledgerService.Balance(ctx, locked.CashLedgerAccountID.String(), "USD", ledger.DimensionSettled)
	if err != nil {
		return Statement{}, err
	}
	withdrawable, err := ledgerService.Balance(ctx, locked.CashLedgerAccountID.String(), "USD", ledger.DimensionWithdrawable)
	if err != nil {
		return Statement{}, err
	}
	receivable, err := ledgerService.Balance(ctx, locked.ReceivableLedgerAccountID.String(), "USD", ledger.DimensionReceivable)
	if err != nil {
		return Statement{}, err
	}
	if minimum(positiveRemainder(settled), positiveRemainder(withdrawable)).Compare(statement.AmountDueUsd) < 0 ||
		positiveRemainder(receivable).Compare(statement.AmountDueUsd) < 0 {
		return Statement{}, ErrInsufficientPower
	}
	posting, err := service.postStatementRepayment(ctx, tx, locked, statement.ID.String(), statement.AmountDueUsd)
	if err != nil {
		return Statement{}, err
	}
	paid, err := queries.MarkStatementPaid(ctx, store.MarkStatementPaidParams{
		ID: statement.ID, RepaymentLedgerTransactionID: uuidValue(posting.TransactionID),
	})
	if err != nil {
		return Statement{}, err
	}
	payload, _ := json.Marshal(map[string]string{"statement_id": paid.ID.String(), "amount_paid_usd": paid.AmountDueUsd.String()})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.statement.paid", ResourceType: "card.statement", ResourceID: paid.ID.String(),
		ActorType: "USER", ActorID: profile.CustomerReference, CorrelationID: paid.ID, Metadata: payload,
		AggregateType: "card.statement", AggregateID: paid.ID.String(), EventType: "card.statement.paid", Payload: payload,
	}); err != nil {
		return Statement{}, err
	}
	if _, err := service.notifications.Record(ctx, tx, notification.Event{
		NotificationEventID: "card.statement.paid." + paid.ID.String(), CustomerReference: profile.CustomerReference,
		Category: "CARD", EventType: "card.statement.paid", TitleKey: "notification.card.statement.paid.title",
		BodyKey: "notification.card.statement.paid.body", ResourceType: "card.statement", ResourceID: paid.ID.String(), ActionPath: "/card/statements",
	}); err != nil {
		return Statement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Statement{}, err
	}
	return statementFromStore(paid), nil
}

func (service *Service) ListStatements(ctx context.Context, accessToken string) ([]Statement, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(service.database).ListStatements(ctx, profile.CustomerReference)
	if err != nil {
		return nil, err
	}
	result := make([]Statement, 0, len(rows))
	for _, row := range rows {
		result = append(result, statementFromStore(row))
	}
	return result, nil
}

func statementFromStore(value store.CardStatement) Statement {
	var paidAt *time.Time
	if value.PaidAt.Valid {
		paid := value.PaidAt.Time.UTC()
		paidAt = &paid
	}
	return Statement{
		ID: value.ID.String(), CustomerReference: value.CustomerReference,
		PeriodStart: value.PeriodStart.Time.Format("2006-01-02"), PeriodEnd: value.PeriodEnd.Time.Format("2006-01-02"),
		AmountDueUSD: value.AmountDueUsd, Status: value.Status, DueAt: value.DueAt.Time.UTC(), PaidAt: paidAt,
		RepaymentLedgerTransactionID: value.RepaymentLedgerTransactionID.String(), CreatedAt: value.CreatedAt.Time.UTC(),
	}
}

func utcDate(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func dateValue(value time.Time) pgtype.Date {
	return pgtype.Date{Time: utcDate(value), Valid: !value.IsZero()}
}

func authorizedStatementAdmin(actor AdminActor) bool {
	return actor.ID != "" && (actor.Role == "OPERATIONS" || actor.Role == "ADMIN")
}
