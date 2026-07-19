package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

func TestLocalContract(t *testing.T) {
	t.Parallel()
	adapter, err := NewLocal(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Name() == "" || adapter.Capabilities(t.Context()).Mode != "SIMULATED" || adapter.Health(t.Context()) != nil {
		t.Fatal("local notification provider contract is incomplete")
	}
	result, err := adapter.Deliver(t.Context(), Delivery{
		NotificationEventID: "card.auth.0001", CustomerReference: "fixture:card-user",
		Channel: "PUSH", TitleKey: "notification.card.auth.title", BodyKey: "notification.card.auth.body",
	})
	if err != nil || result.Status != "SENT" {
		t.Fatalf("unexpected delivery: %+v %v", result, err)
	}
}

func TestLocalRejectsProductionAndInvalidDelivery(t *testing.T) {
	t.Parallel()
	if _, err := NewLocal(config.EnvironmentProduction); !errors.Is(err, ErrProductionMode) {
		t.Fatalf("expected production rejection, got %v", err)
	}
	adapter, _ := NewLocal(config.EnvironmentTest)
	if _, err := adapter.Deliver(context.Background(), Delivery{}); !errors.Is(err, ErrInvalidDelivery) {
		t.Fatalf("expected invalid delivery, got %v", err)
	}
}
