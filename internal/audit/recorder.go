package audit

import (
	"context"
	"fmt"

	"github.com/KDTikkly/Cytisus/internal/audit/store"
	"github.com/jackc/pgx/v5/pgtype"
)

type Event struct {
	Action        string
	ResourceType  string
	ResourceID    string
	ActorType     string
	ActorID       string
	CorrelationID pgtype.UUID
	Metadata      []byte
}

func Record(ctx context.Context, database store.DBTX, event Event) error {
	if database == nil {
		return fmt.Errorf("record audit event: database is required")
	}
	if _, err := store.New(database).InsertAuditEvent(ctx, store.InsertAuditEventParams{
		Action:        event.Action,
		ResourceType:  event.ResourceType,
		ResourceID:    event.ResourceID,
		ActorType:     event.ActorType,
		ActorID:       event.ActorID,
		CorrelationID: event.CorrelationID,
		Metadata:      event.Metadata,
	}); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}
