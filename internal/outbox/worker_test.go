package outbox

import (
	"errors"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/outbox/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestWorkerConfigValidation(t *testing.T) {
	t.Parallel()
	valid := Config{
		WorkerID:       "worker-001",
		BatchSize:      25,
		LeaseDuration:  30 * time.Second,
		BaseBackoff:    time.Second,
		MaximumBackoff: time.Hour,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.BaseBackoff = 1500 * time.Millisecond
	if !errors.Is(invalid.Validate(), ErrInvalidConfiguration) {
		t.Fatalf("expected invalid duration error, got %v", invalid.Validate())
	}
	invalid = valid
	invalid.BatchSize = 1001
	if !errors.Is(invalid.Validate(), ErrInvalidConfiguration) {
		t.Fatalf("expected invalid batch error, got %v", invalid.Validate())
	}
}

type privateHandlerError struct{}

func (privateHandlerError) Error() string    { return "customer-secret-must-not-persist" }
func (privateHandlerError) SafeCode() string { return "provider.timeout" }

func TestSafeErrorCodeDoesNotPersistArbitraryMessages(t *testing.T) {
	t.Parallel()
	if code := safeErrorCode(privateHandlerError{}); code != "provider.timeout" {
		t.Fatalf("expected safe provider code, got %s", code)
	}
	if code := safeErrorCode(errors.New("private payload")); code != "handler.failed" {
		t.Fatalf("expected generic code, got %s", code)
	}
}

func TestEventConversionCopiesPayload(t *testing.T) {
	t.Parallel()
	stored := store.OutboxEvent{
		ID:            mustTestUUID(t, "00000000-0000-4000-8000-000000000010"),
		AggregateType: "ledger.transaction",
		AggregateID:   "aggregate-1",
		EventType:     "ledger.transaction.posted",
		EventVersion:  1,
		Payload:       []byte(`{"ok":true}`),
		AttemptCount:  2,
		MaxAttempts:   8,
	}
	event := eventFromStore(stored)
	stored.Payload[0] = 'x'
	if string(event.Payload) != `{"ok":true}` {
		t.Fatalf("payload alias detected: %s", event.Payload)
	}
}

func TestStatusValidation(t *testing.T) {
	t.Parallel()
	for _, status := range []Status{StatusPending, StatusProcessing, StatusDelivered, StatusDeadLetter, StatusCancelled} {
		if !validStatus(status) {
			t.Fatalf("expected %s to be valid", status)
		}
	}
	if validStatus("UNKNOWN") {
		t.Fatal("unexpected unknown status acceptance")
	}
}

func mustTestUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	identifier, err := parseEventID(value)
	if err != nil {
		t.Fatal(err)
	}
	return identifier
}
