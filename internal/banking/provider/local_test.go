package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

func TestLocalProviderIsDeterministicAndSimulated(t *testing.T) {
	adapter, err := NewLocal(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	request := TransferRequest{
		ClientReference:          "funding-request-0001",
		ExternalAccountReference: "fixture-bank-001",
		Rail:                     RailACH,
		Amount:                   money.MustParse("125.50"),
	}
	first, err := adapter.InitiateFunding(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.InitiateFunding(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !first.Simulated || first.InitialStatus != "INITIATED" {
		t.Fatalf("unexpected deterministic response: %#v %#v", first, second)
	}
	health, err := adapter.Health(context.Background())
	if err != nil || !health.Available || health.Mode != "SIMULATED" {
		t.Fatalf("unexpected health: %#v %v", health, err)
	}
}

func TestLocalProviderTimeoutAndProductionGuard(t *testing.T) {
	if _, err := NewLocal(config.EnvironmentProduction); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected production guard, got %v", err)
	}
	adapter, _ := NewLocal(config.EnvironmentTest)
	_, err := adapter.PrepareWithdrawal(context.Background(), TransferRequest{
		ClientReference:          "withdrawal-request-0001",
		ExternalAccountReference: "fixture-timeout",
		Rail:                     RailACH,
		Amount:                   money.MustParse("10"),
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected timeout, got %v", err)
	}
}
