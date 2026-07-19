package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KDTikkly/Cytisus/internal/outbox/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type AdminService struct {
	database database
}

func NewAdminService(database database) *AdminService {
	return &AdminService{database: database}
}

func (service *AdminService) List(ctx context.Context, status Status, pageSize, pageOffset int32) ([]Event, error) {
	if service == nil || service.database == nil || !validStatus(status) || pageSize <= 0 || pageSize > 200 || pageOffset < 0 {
		return nil, ErrInvalidConfiguration
	}
	stored, err := store.New(service.database).ListOutboxEventsByStatus(ctx, store.ListOutboxEventsByStatusParams{
		Status:     string(status),
		PageOffset: pageOffset,
		PageSize:   pageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list outbox events: %w", err)
	}
	events := make([]Event, 0, len(stored))
	for _, event := range stored {
		events = append(events, eventFromStore(event))
	}
	return events, nil
}

func (service *AdminService) Replay(ctx context.Context, eventID, actorID, reasonCode string) (Event, error) {
	return service.mutate(ctx, eventID, actorID, reasonCode, "outbox.event.replayed", func(ctx context.Context, queries *store.Queries, identifier pgtype.UUID) (store.OutboxEvent, error) {
		return queries.ReplayDeadLetterEvent(ctx, identifier)
	})
}

func (service *AdminService) Cancel(ctx context.Context, eventID, actorID, reasonCode string) (Event, error) {
	return service.mutate(ctx, eventID, actorID, reasonCode, "outbox.event.cancelled", func(ctx context.Context, queries *store.Queries, identifier pgtype.UUID) (store.OutboxEvent, error) {
		return queries.CancelOutboxEvent(ctx, identifier)
	})
}

type adminMutation func(context.Context, *store.Queries, pgtype.UUID) (store.OutboxEvent, error)

func (service *AdminService) mutate(ctx context.Context, eventID, actorID, reasonCode, action string, mutation adminMutation) (Event, error) {
	if service == nil || service.database == nil || actorID == "" || !codePattern.MatchString(reasonCode) {
		return Event{}, ErrInvalidConfiguration
	}
	identifier, err := parseEventID(eventID)
	if err != nil {
		return Event{}, ErrInvalidConfiguration
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Event{}, fmt.Errorf("begin outbox admin mutation: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	mutated, err := mutation(ctx, queries, identifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrInvalidState
	}
	if err != nil {
		return Event{}, fmt.Errorf("mutate outbox event: %w", err)
	}
	metadata, err := json.Marshal(struct {
		ReasonCode string `json:"reason_code"`
	}{ReasonCode: reasonCode})
	if err != nil {
		return Event{}, fmt.Errorf("encode outbox audit metadata: %w", err)
	}
	if _, err := queries.InsertOutboxAdminAudit(ctx, store.InsertOutboxAdminAuditParams{
		Action:        action,
		ResourceID:    eventID,
		ActorID:       actorID,
		CorrelationID: identifier,
		Metadata:      metadata,
	}); err != nil {
		return Event{}, fmt.Errorf("insert outbox admin audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Event{}, fmt.Errorf("commit outbox admin mutation: %w", err)
	}
	return eventFromStore(mutated), nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusPending, StatusProcessing, StatusDelivered, StatusDeadLetter, StatusCancelled:
		return true
	default:
		return false
	}
}
