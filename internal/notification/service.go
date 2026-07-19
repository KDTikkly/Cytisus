package notification

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/notification/provider"
	"github.com/KDTikkly/Cytisus/internal/notification/store"
	outboxstore "github.com/KDTikkly/Cytisus/internal/outbox/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Event struct {
	NotificationEventID string
	CustomerReference   string
	Category            string
	EventType           string
	TitleKey            string
	BodyKey             string
	ResourceType        string
	ResourceID          string
	ActionPath          string
	Channels            []string
}

type InboxItem struct {
	ID                  string
	NotificationEventID string
	Category            string
	EventType           string
	TitleKey            string
	BodyKey             string
	ResourceType        string
	ResourceID          string
	ActionPath          string
	DeliveryStatus      string
	CreatedAt           time.Time
}

type Service struct {
	provider provider.Adapter
}

func NewService(adapter provider.Adapter) (*Service, error) {
	if adapter == nil {
		return nil, errors.New("notification provider is required")
	}
	return &Service{provider: adapter}, nil
}

func (service *Service) Record(ctx context.Context, tx pgx.Tx, event Event) (bool, error) {
	if tx == nil || strings.TrimSpace(event.NotificationEventID) == "" || strings.TrimSpace(event.CustomerReference) == "" ||
		event.Category == "" || event.EventType == "" || event.TitleKey == "" || event.BodyKey == "" ||
		event.ResourceType == "" || event.ResourceID == "" {
		return false, errors.New("invalid notification event")
	}
	if len(event.Channels) == 0 {
		event.Channels = []string{"IN_APP", "PUSH"}
	}
	identifier, err := newUUID()
	if err != nil {
		return false, err
	}
	queries := store.New(tx)
	created, err := queries.InsertEvent(ctx, store.InsertEventParams{
		ID: identifier, NotificationEventID: event.NotificationEventID, CustomerReference: event.CustomerReference,
		Category: event.Category, EventType: event.EventType, TitleKey: event.TitleKey, BodyKey: event.BodyKey,
		ResourceType: event.ResourceType, ResourceID: event.ResourceID, ActionPath: textValue(event.ActionPath),
	})
	replayed := false
	if errors.Is(err, pgx.ErrNoRows) {
		created, err = queries.GetEventByKey(ctx, event.NotificationEventID)
		replayed = true
	}
	if err != nil {
		return false, fmt.Errorf("record notification event: %w", err)
	}
	for _, channel := range event.Channels {
		result, deliverErr := service.provider.Deliver(ctx, provider.Delivery{
			NotificationEventID: event.NotificationEventID, CustomerReference: event.CustomerReference,
			Channel: channel, TitleKey: event.TitleKey, BodyKey: event.BodyKey,
		})
		if deliverErr != nil {
			return false, fmt.Errorf("deliver local notification: %w", deliverErr)
		}
		_, insertErr := queries.InsertDelivery(ctx, store.InsertDeliveryParams{
			EventID: created.ID, Channel: channel, Provider: service.provider.Name(), Status: result.Status,
			DeliveredAt: timestamptz(time.Now().UTC()),
		})
		if insertErr != nil && !errors.Is(insertErr, pgx.ErrNoRows) {
			return false, fmt.Errorf("record notification delivery: %w", insertErr)
		}
	}
	if !replayed {
		payload := []byte(fmt.Sprintf(`{"notification_event_id":%q,"event_type":%q}`, event.NotificationEventID, event.EventType))
		if _, err := outboxstore.New(tx).InsertOutboxEvent(ctx, outboxstore.InsertOutboxEventParams{
			AggregateType: "notification.event", AggregateID: created.ID.String(), EventType: "notification.event.created",
			EventVersion: 1, Payload: payload, MaxAttempts: 8,
		}); err != nil {
			return false, fmt.Errorf("insert notification outbox event: %w", err)
		}
	}
	return replayed, nil
}

func ListInbox(ctx context.Context, database store.DBTX, customerReference string, pageSize int32) ([]InboxItem, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(database).ListInbox(ctx, store.ListInboxParams{CustomerReference: customerReference, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("list notification inbox: %w", err)
	}
	items := make([]InboxItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, InboxItem{
			ID: row.ID.String(), NotificationEventID: row.NotificationEventID, Category: row.Category,
			EventType: row.EventType, TitleKey: row.TitleKey, BodyKey: row.BodyKey,
			ResourceType: row.ResourceType, ResourceID: row.ResourceID, ActionPath: row.ActionPath.String,
			DeliveryStatus: row.DeliveryStatus, CreatedAt: row.CreatedAt.Time.UTC(),
		})
	}
	return items, nil
}

func newUUID() (pgtype.UUID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return pgtype.UUID{}, fmt.Errorf("generate notification UUID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return pgtype.UUID{Bytes: value, Valid: true}, nil
}

func textValue(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: !value.IsZero()}
}
