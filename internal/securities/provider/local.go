package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
)

const LocalPaperBrokerProvider = "paper-broker-simulator"

var splitPolicy = money.RoundingPolicy{
	Version:       "paper-fill-split-v1",
	DecimalPlaces: money.Scale,
	Mode:          money.RoundTowardZero,
}

type Local struct{}

func NewLocal(environment config.Environment) (*Local, error) {
	if environment != config.EnvironmentLocal && environment != config.EnvironmentTest {
		return nil, broker.ErrSimulatorDisabled
	}
	return &Local{}, nil
}

func (*Local) Name() string { return LocalPaperBrokerProvider }

func (*Local) Capabilities(context.Context) broker.Capabilities {
	return broker.Capabilities{
		Mode:                "SIMULATED",
		MarketOrders:        true,
		LimitOrders:         true,
		DayOrders:           true,
		GTCOrders:           true,
		Fractional:          true,
		PartialFills:        true,
		DeterministicReplay: true,
	}
}

func (*Local) Health(ctx context.Context) error { return ctx.Err() }

func (*Local) Execute(ctx context.Context, request broker.Request) (broker.Event, error) {
	if err := ctx.Err(); err != nil {
		return broker.Event{}, err
	}
	if err := validateRequest(request); err != nil {
		return broker.Event{}, err
	}

	event := broker.Event{
		Provider:        LocalPaperBrokerProvider,
		ProviderOrderID: "paper-" + request.ClientOrderReference[:24],
		ReplayCursor:    request.ReplayCursor,
		OccurredAt:      request.Quote.ObservedAt.UTC(),
	}
	remaining, err := request.Quantity.Sub(request.FilledQuantity)
	if err != nil || remaining.Sign() <= 0 {
		return broker.Event{}, broker.ErrInvalidRequest
	}

	fillPrice, marketable := executablePrice(request)
	if marketable {
		event.Type = broker.EventFill
		event.FillSequence = request.ReplayCursor + 1
		event.FillPrice = fillPrice
		if request.FilledQuantity.IsZero() {
			event.FillQuantity, err = remaining.Divide(money.MustParse("2"), splitPolicy)
			if err != nil {
				return broker.Event{}, fmt.Errorf("split deterministic fill: %w", err)
			}
			if event.FillQuantity.IsZero() {
				event.FillQuantity = remaining
			}
		} else {
			event.FillQuantity = remaining
		}
	} else if request.TimeInForce == broker.TimeInForceDay && request.ReplayCursor >= 2 {
		event.Type = broker.EventOrderExpired
	} else {
		event.Type = broker.EventOrderOpened
	}

	event.ExternalEventID = externalEventID(request.ClientOrderReference, event.ReplayCursor, event.Type)
	payload, err := json.Marshal(struct {
		ClientOrderReference string `json:"client_order_reference"`
		EventType            string `json:"event_type"`
		FillPrice            string `json:"fill_price,omitempty"`
		FillQuantity         string `json:"fill_quantity,omitempty"`
		ProviderOrderID      string `json:"provider_order_id"`
		ReplayCursor         int32  `json:"replay_cursor"`
	}{
		ClientOrderReference: request.ClientOrderReference,
		EventType:            string(event.Type),
		FillPrice:            emptyWhenZero(event.FillPrice),
		FillQuantity:         emptyWhenZero(event.FillQuantity),
		ProviderOrderID:      event.ProviderOrderID,
		ReplayCursor:         event.ReplayCursor,
	})
	if err != nil {
		return broker.Event{}, fmt.Errorf("encode broker event: %w", err)
	}
	event.Payload = payload
	return event, nil
}

func validateRequest(request broker.Request) error {
	if len(request.ClientOrderReference) != 64 || strings.TrimSpace(request.Symbol) == "" ||
		!request.Quantity.IsPositive() || request.FilledQuantity.Sign() < 0 ||
		request.FilledQuantity.Compare(request.Quantity) >= 0 || request.ReplayCursor < 0 ||
		request.Quote.Status != marketdata.QuoteStatusSimulated {
		return broker.ErrInvalidRequest
	}
	if request.Side != broker.SideBuy && request.Side != broker.SideSell {
		return broker.ErrInvalidRequest
	}
	if request.TimeInForce != broker.TimeInForceDay && request.TimeInForce != broker.TimeInForceGTC {
		return broker.ErrInvalidRequest
	}
	if request.OrderType == broker.OrderTypeMarket {
		if request.LimitPrice != nil {
			return broker.ErrInvalidRequest
		}
		return nil
	}
	if request.OrderType != broker.OrderTypeLimit || request.LimitPrice == nil || !request.LimitPrice.IsPositive() {
		return broker.ErrInvalidRequest
	}
	return nil
}

func executablePrice(request broker.Request) (money.Decimal, bool) {
	if request.Side == broker.SideBuy {
		if !request.Quote.Ask.IsPositive() {
			return money.Zero(), false
		}
		return request.Quote.Ask, request.OrderType == broker.OrderTypeMarket || request.LimitPrice.Compare(request.Quote.Ask) >= 0
	}
	if !request.Quote.Bid.IsPositive() {
		return money.Zero(), false
	}
	return request.Quote.Bid, request.OrderType == broker.OrderTypeMarket || request.LimitPrice.Compare(request.Quote.Bid) <= 0
}

func externalEventID(reference string, cursor int32, eventType broker.EventType) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", reference, cursor, eventType)))
	return hex.EncodeToString(digest[:])
}

func emptyWhenZero(value money.Decimal) string {
	if value.IsZero() {
		return ""
	}
	return value.String()
}
