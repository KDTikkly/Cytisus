package outbox

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/KDTikkly/Cytisus/internal/outbox/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusDelivered  Status = "DELIVERED"
	StatusDeadLetter Status = "DEAD_LETTER"
	StatusCancelled  Status = "CANCELLED"
)

var (
	ErrInvalidConfiguration = errors.New("invalid outbox configuration")
	ErrInvalidState         = errors.New("outbox event is not in the required state")
	identifierPattern       = regexp.MustCompile(`^[a-z][a-z0-9._:-]{2,127}$`)
)

type database interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type Config struct {
	WorkerID       string
	BatchSize      int32
	LeaseDuration  time.Duration
	BaseBackoff    time.Duration
	MaximumBackoff time.Duration
}

func (config Config) Validate() error {
	if !identifierPattern.MatchString(config.WorkerID) || config.BatchSize <= 0 || config.BatchSize > 1000 {
		return ErrInvalidConfiguration
	}
	for _, duration := range []time.Duration{config.LeaseDuration, config.BaseBackoff, config.MaximumBackoff} {
		if duration <= 0 || duration%time.Second != 0 || duration/time.Second > time.Duration(2147483647) {
			return ErrInvalidConfiguration
		}
	}
	if config.BaseBackoff > config.MaximumBackoff {
		return ErrInvalidConfiguration
	}
	return nil
}

type Event struct {
	ID              string
	AggregateType   string
	AggregateID     string
	Type            string
	Version         int32
	Payload         []byte
	Status          Status
	Attempt         int32
	MaximumAttempts int32
	AvailableAt     time.Time
	CreatedAt       time.Time
	LastErrorCode   string
}

// TransactionalHandler must write every local consumer side effect through the
// supplied transaction. The worker commits that effect together with the
// consumer receipt and DELIVERED state.
type TransactionalHandler interface {
	ConsumerName() string
	Handle(context.Context, pgx.Tx, Event) error
}

// SafeError allows a handler to expose a non-sensitive stable code for durable
// retry diagnostics. Arbitrary error messages are never persisted.
type SafeError interface {
	error
	SafeCode() string
}

type Worker struct {
	database database
	config   Config
}

type RunSummary struct {
	Claimed      int
	Delivered    int
	Retried      int
	DeadLettered int
	Deduplicated int
}

func NewWorker(database database, config Config) (*Worker, error) {
	if database == nil {
		return nil, ErrInvalidConfiguration
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Worker{database: database, config: config}, nil
}

func (worker *Worker) RunOnce(ctx context.Context, handler TransactionalHandler) (RunSummary, error) {
	if worker == nil || worker.database == nil || handler == nil || !identifierPattern.MatchString(handler.ConsumerName()) {
		return RunSummary{}, ErrInvalidConfiguration
	}
	claimed, expired, err := worker.claim(ctx)
	if err != nil {
		return RunSummary{}, err
	}
	summary := RunSummary{Claimed: len(claimed), DeadLettered: int(expired)}
	var runErrors []error
	for _, storedEvent := range claimed {
		event := eventFromStore(storedEvent)
		deduplicated, processErr := worker.process(ctx, handler, event, storedEvent.ID)
		if processErr == nil {
			summary.Delivered++
			if deduplicated {
				summary.Deduplicated++
			}
			continue
		}
		status, failErr := worker.fail(ctx, storedEvent.ID, safeErrorCode(processErr))
		if failErr != nil {
			runErrors = append(runErrors, errors.Join(processErr, failErr))
			continue
		}
		if status == StatusDeadLetter {
			summary.DeadLettered++
		} else {
			summary.Retried++
		}
		runErrors = append(runErrors, fmt.Errorf("handle outbox event %s: %w", event.ID, processErr))
	}
	return summary, errors.Join(runErrors...)
}

func (worker *Worker) claim(ctx context.Context) ([]store.OutboxEvent, int64, error) {
	tx, err := worker.database.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	expired, err := queries.DeadLetterExpiredOutboxClaims(ctx, int32(worker.config.LeaseDuration/time.Second))
	if err != nil {
		return nil, 0, fmt.Errorf("dead-letter expired outbox claims: %w", err)
	}
	claimed, err := queries.ClaimOutboxEvents(ctx, store.ClaimOutboxEventsParams{
		WorkerID:     pgtype.Text{String: worker.config.WorkerID, Valid: true},
		LeaseSeconds: int32(worker.config.LeaseDuration / time.Second),
		BatchSize:    worker.config.BatchSize,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("claim outbox events: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit outbox claim: %w", err)
	}
	return claimed, expired, nil
}

func (worker *Worker) process(ctx context.Context, handler TransactionalHandler, event Event, eventID pgtype.UUID) (bool, error) {
	tx, err := worker.database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin outbox delivery: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	_, err = queries.InsertConsumerReceipt(ctx, store.InsertConsumerReceiptParams{
		ConsumerName: handler.ConsumerName(),
		EventID:      eventID,
	})
	deduplicated := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !deduplicated {
		return false, fmt.Errorf("insert consumer receipt: %w", err)
	}
	if !deduplicated {
		if err := handler.Handle(ctx, tx, event); err != nil {
			return false, err
		}
	}
	if _, err := queries.MarkOutboxDelivered(ctx, store.MarkOutboxDeliveredParams{
		ID:       eventID,
		WorkerID: pgtype.Text{String: worker.config.WorkerID, Valid: true},
	}); err != nil {
		return false, fmt.Errorf("mark outbox delivered: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit outbox delivery: %w", err)
	}
	return deduplicated, nil
}

func (worker *Worker) fail(ctx context.Context, eventID pgtype.UUID, errorCode string) (Status, error) {
	tx, err := worker.database.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin outbox failure: %w", err)
	}
	defer tx.Rollback(ctx)
	failed, err := store.New(tx).MarkOutboxFailed(ctx, store.MarkOutboxFailedParams{
		MaxBackoffSeconds:  int32(worker.config.MaximumBackoff / time.Second),
		BaseBackoffSeconds: int64(worker.config.BaseBackoff / time.Second),
		ErrorMessage:       errorCode,
		ID:                 eventID,
		WorkerID:           pgtype.Text{String: worker.config.WorkerID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidState
	}
	if err != nil {
		return "", fmt.Errorf("mark outbox failed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit outbox failure: %w", err)
	}
	return Status(failed.Status), nil
}

func eventFromStore(event store.OutboxEvent) Event {
	return Event{
		ID:              event.ID.String(),
		AggregateType:   event.AggregateType,
		AggregateID:     event.AggregateID,
		Type:            event.EventType,
		Version:         event.EventVersion,
		Payload:         append([]byte(nil), event.Payload...),
		Status:          Status(event.Status),
		Attempt:         event.AttemptCount,
		MaximumAttempts: event.MaxAttempts,
		AvailableAt:     event.AvailableAt.Time,
		CreatedAt:       event.CreatedAt.Time,
		LastErrorCode:   event.LastError.String,
	}
}

func safeErrorCode(err error) string {
	var safe SafeError
	if errors.As(err, &safe) && identifierPattern.MatchString(safe.SafeCode()) {
		return safe.SafeCode()
	}
	return "handler.failed"
}
