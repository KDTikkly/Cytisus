package crypto

import (
	"context"
	"fmt"

	"github.com/KDTikkly/Cytisus/internal/audit"
	outboxstore "github.com/KDTikkly/Cytisus/internal/outbox/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type mutation struct {
	Action        string
	ResourceType  string
	ResourceID    string
	ActorType     string
	ActorID       string
	CorrelationID pgtype.UUID
	Metadata      []byte
	AggregateType string
	AggregateID   string
	EventType     string
	EventVersion  int32
	Payload       []byte
}

func recordMutation(ctx context.Context, tx pgx.Tx, event mutation) error {
	if err := audit.Record(ctx, tx, audit.Event{
		Action: event.Action, ResourceType: event.ResourceType, ResourceID: event.ResourceID,
		ActorType: event.ActorType, ActorID: event.ActorID, CorrelationID: event.CorrelationID, Metadata: event.Metadata,
	}); err != nil {
		return err
	}
	version := event.EventVersion
	if version <= 0 {
		version = 1
	}
	if _, err := outboxstore.New(tx).InsertOutboxEvent(ctx, outboxstore.InsertOutboxEventParams{
		AggregateType: event.AggregateType, AggregateID: event.AggregateID, EventType: event.EventType,
		EventVersion: version, Payload: event.Payload, MaxAttempts: 8,
	}); err != nil {
		return fmt.Errorf("insert crypto outbox event: %w", err)
	}
	return nil
}
