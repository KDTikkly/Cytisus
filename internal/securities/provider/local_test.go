package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
)

func TestLocalMarketOrderProducesDeterministicPartialThenFinalFill(t *testing.T) {
	adapter, err := NewLocal(config.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	request := broker.Request{
		ClientOrderReference: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Symbol:               "AAPL",
		Side:                 broker.SideBuy,
		OrderType:            broker.OrderTypeMarket,
		TimeInForce:          broker.TimeInForceDay,
		Quantity:             money.MustParse("1.5"),
		ReplayCursor:         0,
		Quote: marketdata.Quote{
			Ask:        money.MustParse("190.2"),
			Bid:        money.MustParse("190.1"),
			Status:     marketdata.QuoteStatusSimulated,
			ObservedAt: time.Date(2026, 7, 19, 9, 30, 0, 0, time.UTC),
		},
	}
	first, err := adapter.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Type != broker.EventFill || first.FillQuantity.String() != "0.75" || first.FillPrice.String() != "190.2" {
		t.Fatalf("unexpected first event: %+v", first)
	}

	request.FilledQuantity = first.FillQuantity
	request.ReplayCursor = 1
	request.Quote.ReplayCursor = 1
	second, err := adapter.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Type != broker.EventFill || second.FillQuantity.String() != "0.75" || first.ExternalEventID == second.ExternalEventID {
		t.Fatalf("unexpected second event: %+v", second)
	}
}

func TestLocalNonMarketableLimitOpensThenDayExpires(t *testing.T) {
	adapter, _ := NewLocal(config.EnvironmentTest)
	limit := money.MustParse("100")
	request := broker.Request{
		ClientOrderReference: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Symbol:               "AAPL",
		Side:                 broker.SideBuy,
		OrderType:            broker.OrderTypeLimit,
		TimeInForce:          broker.TimeInForceDay,
		Quantity:             money.MustParse("2"),
		LimitPrice:           &limit,
		ReplayCursor:         0,
		Quote: marketdata.Quote{
			Ask:        money.MustParse("190.2"),
			Bid:        money.MustParse("190.1"),
			Status:     marketdata.QuoteStatusSimulated,
			ObservedAt: time.Now().UTC(),
		},
	}
	opened, err := adapter.Execute(context.Background(), request)
	if err != nil || opened.Type != broker.EventOrderOpened {
		t.Fatalf("expected open event, got %+v, %v", opened, err)
	}
	request.ReplayCursor = 2
	expired, err := adapter.Execute(context.Background(), request)
	if err != nil || expired.Type != broker.EventOrderExpired {
		t.Fatalf("expected expiry event, got %+v, %v", expired, err)
	}
}

func TestLocalSimulatorIsEnvironmentGated(t *testing.T) {
	if _, err := NewLocal(config.EnvironmentProduction); !errors.Is(err, broker.ErrSimulatorDisabled) {
		t.Fatalf("expected simulator disabled, got %v", err)
	}
}
