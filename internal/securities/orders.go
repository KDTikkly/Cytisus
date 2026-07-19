package securities

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
	"github.com/KDTikkly/Cytisus/internal/securities/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) SearchInstruments(ctx context.Context, query string, pageSize int32) ([]marketdata.Instrument, error) {
	return service.catalog.Search(ctx, query, pageSize)
}

func (service *Service) Quote(ctx context.Context, symbol string, replayCursor int32) (marketdata.Quote, error) {
	quote, err := service.quotes.Quote(ctx, symbol, replayCursor)
	if errors.Is(err, marketdata.ErrQuoteUnavailable) {
		return marketdata.Quote{}, ErrQuoteUnavailable
	}
	return quote, err
}

func (service *Service) SubmitOrder(ctx context.Context, command SubmitOrderCommand) (Order, error) {
	account, err := service.authenticate(ctx, command.AccessToken)
	if err != nil {
		return Order{}, err
	}
	command.Symbol = strings.ToUpper(strings.TrimSpace(command.Symbol))
	requestHash, err := validateAndHashSubmit(command)
	if err != nil {
		return Order{}, err
	}
	instrument, err := service.catalog.GetBySymbol(ctx, command.Symbol)
	if err != nil {
		return Order{}, err
	}
	if !instrument.Capability.PaperTradable {
		return Order{}, fmt.Errorf("%w: %s", ErrInstrumentViewOnly, instrument.Capability.DisabledReason)
	}
	if !instrument.Capability.FractionalEnabled && !command.Quantity.IsInteger() {
		return Order{}, ErrFractionalUnsupported
	}
	quote, err := service.Quote(ctx, command.Symbol, 0)
	if err != nil {
		return Order{}, err
	}
	if quote.Status == marketdata.QuoteStatusStale {
		return Order{}, ErrQuoteStale
	}
	if quote.Status != marketdata.QuoteStatusSimulated {
		return Order{}, ErrQuoteUnavailable
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("begin submit order: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.LockPaperAccount(ctx, account.ID); err != nil {
		return Order{}, fmt.Errorf("lock paper account: %w", err)
	}
	orderID, err := newUUID()
	if err != nil {
		return Order{}, err
	}
	insertedID, err := queries.InsertOrderRequest(ctx, store.InsertOrderRequestParams{
		PaperAccountID: account.ID,
		IdempotencyKey: command.IdempotencyKey,
		RequestHash:    requestHash,
		OrderID:        orderID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetOrderRequest(ctx, store.GetOrderRequestParams{
			PaperAccountID: account.ID,
			IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil {
			return Order{}, fmt.Errorf("get order idempotency: %w", getErr)
		}
		if existingRequest.RequestHash != requestHash {
			return Order{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetOrder(ctx, existingRequest.OrderID)
		if getErr != nil {
			return Order{}, fmt.Errorf("get replayed order: %w", getErr)
		}
		fills, getErr := queries.ListFillsForOrder(ctx, existing.ID)
		if getErr != nil {
			return Order{}, fmt.Errorf("list replayed fills: %w", getErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return Order{}, fmt.Errorf("commit replayed order: %w", err)
		}
		return orderFromStore(existing, fills), nil
	}
	if err != nil {
		return Order{}, fmt.Errorf("acquire order idempotency: %w", err)
	}
	if insertedID != orderID {
		return Order{}, ErrIdempotencyConflict
	}
	if err := service.ensureOrderCapacity(ctx, tx, account, instrument, command.Side, command.Quantity, quote, command.LimitPrice); err != nil {
		return Order{}, err
	}

	clientReference := clientOrderReference(account.ID.String(), command.IdempotencyKey)
	brokerEvent, err := service.executeBroker(ctx, broker.Request{
		ClientOrderReference: clientReference,
		Symbol:               instrument.Symbol,
		Side:                 command.Side,
		OrderType:            command.OrderType,
		TimeInForce:          command.TimeInForce,
		Quantity:             command.Quantity,
		LimitPrice:           command.LimitPrice,
		ReplayCursor:         0,
		Quote:                quote,
	})
	if err != nil {
		return Order{}, err
	}
	limitNumeric, err := nullableNumeric(command.LimitPrice)
	if err != nil {
		return Order{}, err
	}
	instrumentID, err := parseUUID(instrument.ID)
	if err != nil {
		return Order{}, err
	}
	created, err := queries.InsertOrder(ctx, store.InsertOrderParams{
		ID:                   orderID,
		PaperAccountID:       account.ID,
		InstrumentID:         instrumentID,
		Symbol:               instrument.Symbol,
		ClientOrderReference: clientReference,
		Side:                 string(command.Side),
		OrderType:            string(command.OrderType),
		TimeInForce:          string(command.TimeInForce),
		Quantity:             command.Quantity,
		LimitPrice:           limitNumeric,
		ReferencePrice:       referencePrice(command.Side, quote),
		QuoteStatus:          string(quote.Status),
		QuoteObservedAt:      pgtype.Timestamptz{Time: quote.ObservedAt.UTC(), Valid: true},
		ReplayCursor:         0,
		ProviderOrderID:      brokerEvent.ProviderOrderID,
		PolicyVersion:        PolicyVersion,
	})
	if err != nil {
		return Order{}, fmt.Errorf("insert order: %w", err)
	}
	updated, err := service.applyBrokerEvent(ctx, tx, account, created, brokerEvent)
	if err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit order: %w", err)
	}
	return service.orderWithFills(ctx, updated)
}

func (service *Service) AdvanceReplay(ctx context.Context, command ActionCommand) (Order, error) {
	return service.applyAction(ctx, command, "ADVANCE_REPLAY")
}

func (service *Service) CancelOrder(ctx context.Context, command ActionCommand) (Order, error) {
	return service.applyAction(ctx, command, "CANCEL")
}

func (service *Service) applyAction(ctx context.Context, command ActionCommand, action string) (Order, error) {
	account, err := service.authenticate(ctx, command.AccessToken)
	if err != nil {
		return Order{}, err
	}
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) {
		return Order{}, fmt.Errorf("order action: %w: invalid idempotency key", ErrInvalidCommand)
	}
	orderID, err := parseUUID(command.OrderID)
	if err != nil {
		return Order{}, ErrOrderNotFound
	}
	requestHash := hashStrings(action, account.ID.String(), command.OrderID)
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("begin order action: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.LockPaperAccount(ctx, account.ID); err != nil {
		return Order{}, fmt.Errorf("lock paper account: %w", err)
	}
	_, err = queries.InsertOrderActionRequest(ctx, store.InsertOrderActionRequestParams{
		PaperAccountID: account.ID,
		IdempotencyKey: command.IdempotencyKey,
		RequestHash:    requestHash,
		OrderID:        orderID,
		Action:         action,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingAction, getErr := queries.GetOrderActionRequest(ctx, store.GetOrderActionRequestParams{
			PaperAccountID: account.ID,
			IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil {
			return Order{}, fmt.Errorf("get action idempotency: %w", getErr)
		}
		if existingAction.RequestHash != requestHash || existingAction.Action != action || existingAction.OrderID != orderID {
			return Order{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetOrder(ctx, orderID)
		if getErr != nil {
			return Order{}, fmt.Errorf("get replayed action order: %w", getErr)
		}
		fills, getErr := queries.ListFillsForOrder(ctx, orderID)
		if getErr != nil {
			return Order{}, fmt.Errorf("list replayed action fills: %w", getErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return Order{}, fmt.Errorf("commit replayed order action: %w", err)
		}
		return orderFromStore(existing, fills), nil
	}
	if err != nil {
		return Order{}, fmt.Errorf("acquire action idempotency: %w", err)
	}
	current, err := queries.GetOrderForUpdate(ctx, store.GetOrderForUpdateParams{ID: orderID, PaperAccountID: account.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("lock order: %w", err)
	}
	if current.Status != string(OrderOpen) && current.Status != string(OrderPartiallyFilled) {
		return Order{}, ErrInvalidOrderState
	}
	if action == "CANCEL" {
		updated, updateErr := queries.MarkOrderCancelled(ctx, orderID)
		if updateErr != nil {
			return Order{}, fmt.Errorf("cancel order: %w", updateErr)
		}
		if err := service.recordOrderMutation(ctx, tx, account, updated, "paper.order.cancelled"); err != nil {
			return Order{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Order{}, fmt.Errorf("commit cancellation: %w", err)
		}
		return service.orderWithFills(ctx, updated)
	}

	nextCursor := current.ReplayCursor + 1
	quote, err := service.Quote(ctx, current.Symbol, nextCursor)
	if errors.Is(err, ErrQuoteUnavailable) {
		return Order{}, ErrReplayExhausted
	}
	if err != nil {
		return Order{}, err
	}
	limitPrice, err := decimalFromNullableNumeric(current.LimitPrice)
	if err != nil {
		return Order{}, err
	}
	event, err := service.executeBroker(ctx, broker.Request{
		ClientOrderReference: current.ClientOrderReference,
		Symbol:               current.Symbol,
		Side:                 broker.Side(current.Side),
		OrderType:            broker.OrderType(current.OrderType),
		TimeInForce:          broker.TimeInForce(current.TimeInForce),
		Quantity:             current.Quantity,
		FilledQuantity:       current.FilledQuantity,
		LimitPrice:           limitPrice,
		ReplayCursor:         nextCursor,
		Quote:                quote,
	})
	if err != nil {
		return Order{}, err
	}
	updated, err := service.applyBrokerEvent(ctx, tx, account, current, event)
	if err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit replay advancement: %w", err)
	}
	return service.orderWithFills(ctx, updated)
}

func (service *Service) executeBroker(ctx context.Context, request broker.Request) (broker.Event, error) {
	providerContext, cancel := context.WithTimeout(ctx, service.providerTimeout)
	defer cancel()
	event, err := service.broker.Execute(providerContext, request)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(providerContext.Err(), context.DeadlineExceeded) {
		return broker.Event{}, broker.ErrProviderTimeout
	}
	if err != nil {
		return broker.Event{}, fmt.Errorf("execute paper broker: %w", err)
	}
	return event, nil
}

func validateAndHashSubmit(command SubmitOrderCommand) (string, error) {
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || command.Symbol == "" || !command.Quantity.IsPositive() {
		return "", fmt.Errorf("submit order: %w", ErrInvalidCommand)
	}
	if command.Side != broker.SideBuy && command.Side != broker.SideSell {
		return "", fmt.Errorf("submit order: %w: invalid side", ErrInvalidCommand)
	}
	if command.TimeInForce != broker.TimeInForceDay && command.TimeInForce != broker.TimeInForceGTC {
		return "", fmt.Errorf("submit order: %w: invalid time in force", ErrInvalidCommand)
	}
	if command.OrderType == broker.OrderTypeMarket && command.LimitPrice != nil {
		return "", fmt.Errorf("submit order: %w: market order cannot have limit price", ErrInvalidCommand)
	}
	if command.OrderType != broker.OrderTypeMarket &&
		(command.OrderType != broker.OrderTypeLimit || command.LimitPrice == nil || !command.LimitPrice.IsPositive()) {
		return "", fmt.Errorf("submit order: %w: invalid limit order", ErrInvalidCommand)
	}
	limit := ""
	if command.LimitPrice != nil {
		limit = command.LimitPrice.String()
	}
	payload, _ := json.Marshal(struct {
		LimitPrice  string `json:"limit_price,omitempty"`
		OrderType   string `json:"order_type"`
		Quantity    string `json:"quantity"`
		Side        string `json:"side"`
		Symbol      string `json:"symbol"`
		TimeInForce string `json:"time_in_force"`
	}{limit, string(command.OrderType), command.Quantity.String(), string(command.Side), command.Symbol, string(command.TimeInForce)})
	return sha256Hex(payload), nil
}

var idempotencyKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$`)

func clientOrderReference(accountID, idempotencyKey string) string {
	return hashStrings("paper-order-v1", accountID, idempotencyKey)
}

func hashStrings(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "|")))
	return hex.EncodeToString(digest[:])
}

func referencePrice(side broker.Side, quote marketdata.Quote) money.Decimal {
	if side == broker.SideBuy {
		return quote.Ask
	}
	return quote.Bid
}
