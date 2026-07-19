package securities

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
	"github.com/KDTikkly/Cytisus/internal/securities/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	notionalPolicy = money.RoundingPolicy{Version: "paper-usd-notional-v1", DecimalPlaces: 2, Mode: money.RoundHalfEven}
	averagePolicy  = money.RoundingPolicy{Version: "paper-position-average-v1", DecimalPlaces: money.Scale, Mode: money.RoundHalfEven}
)

func (service *Service) ensureOrderCapacity(
	ctx context.Context,
	tx pgx.Tx,
	account store.SecuritiesPaperAccount,
	instrument marketdata.Instrument,
	side broker.Side,
	quantity money.Decimal,
	quote marketdata.Quote,
	limitPrice *money.Decimal,
) error {
	if side == broker.SideSell {
		instrumentID, err := parseUUID(instrument.ID)
		if err != nil {
			return err
		}
		position, err := store.New(tx).GetPositionForUpdate(ctx, store.GetPositionForUpdateParams{
			PaperAccountID: account.ID,
			InstrumentID:   instrumentID,
		})
		if errors.Is(err, pgx.ErrNoRows) || err == nil && position.Quantity.Compare(quantity) < 0 {
			return ErrInsufficientPosition
		}
		if err != nil {
			return fmt.Errorf("check sell position: %w", err)
		}
		return nil
	}
	price := quote.Ask
	if limitPrice != nil {
		price = *limitPrice
	}
	notional, err := quantity.Multiply(price, notionalPolicy)
	if err != nil {
		return fmt.Errorf("calculate order notional: %w", err)
	}
	return service.ensureBuyingPower(ctx, tx, account, notional)
}

func (service *Service) ensureBuyingPower(ctx context.Context, tx pgx.Tx, account store.SecuritiesPaperAccount, required money.Decimal) error {
	ledgerService := ledger.NewService(tx)
	settled, err := ledgerService.Balance(ctx, account.CashLedgerAccountID.String(), money.Currency("USD"), ledger.DimensionSettled)
	if err != nil {
		return err
	}
	provisional, err := ledgerService.Balance(ctx, account.CashLedgerAccountID.String(), money.Currency("USD"), ledger.DimensionProvisionalBuying)
	if err != nil {
		return err
	}
	total, err := settled.Add(provisional)
	if err != nil {
		return fmt.Errorf("calculate buying power: %w", err)
	}
	if total.Compare(required) < 0 {
		return ErrInsufficientCash
	}
	return nil
}

func (service *Service) applyBrokerEvent(
	ctx context.Context,
	tx pgx.Tx,
	account store.SecuritiesPaperAccount,
	current store.SecuritiesOrder,
	event broker.Event,
) (store.SecuritiesOrder, error) {
	if event.Provider != service.broker.Name() || event.ProviderOrderID != current.ProviderOrderID ||
		event.ExternalEventID == "" || len(event.Payload) == 0 || event.ReplayCursor < current.ReplayCursor {
		return store.SecuritiesOrder{}, fmt.Errorf("apply broker event: %w", broker.ErrInvalidRequest)
	}
	queries := store.New(tx)
	payloadHash := sha256Hex(event.Payload)
	_, err := queries.InsertBrokerEvent(ctx, store.InsertBrokerEventParams{
		Provider:        event.Provider,
		ExternalEventID: event.ExternalEventID,
		OrderID:         current.ID,
		PayloadHash:     payloadHash,
		EventType:       string(event.Type),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetBrokerEvent(ctx, store.GetBrokerEventParams{
			Provider:        event.Provider,
			ExternalEventID: event.ExternalEventID,
		})
		if getErr != nil {
			return store.SecuritiesOrder{}, fmt.Errorf("get duplicate broker event: %w", getErr)
		}
		if existing.OrderID != current.ID || existing.PayloadHash != payloadHash || existing.EventType != string(event.Type) {
			return store.SecuritiesOrder{}, ledger.ErrProviderEventConflict
		}
		return current, nil
	}
	if err != nil {
		return store.SecuritiesOrder{}, fmt.Errorf("insert broker event: %w", err)
	}

	var updated store.SecuritiesOrder
	switch event.Type {
	case broker.EventOrderOpened:
		if current.Status == string(OrderPendingSubmission) {
			updated, err = queries.MarkOrderOpen(ctx, current.ID)
		} else {
			updated, err = queries.AdvanceOpenOrder(ctx, store.AdvanceOpenOrderParams{ReplayCursor: event.ReplayCursor, ID: current.ID})
		}
	case broker.EventOrderExpired:
		updated, err = queries.MarkOrderExpired(ctx, store.MarkOrderExpiredParams{ReplayCursor: event.ReplayCursor, ID: current.ID})
	case broker.EventOrderRejected:
		code := event.RejectionCode
		if code == "" {
			code = "PROVIDER_REJECTED"
		}
		updated, err = queries.MarkOrderRejected(ctx, store.MarkOrderRejectedParams{
			RejectionCode: pgtype.Text{String: code, Valid: true},
			ReplayCursor:  event.ReplayCursor,
			ID:            current.ID,
		})
	case broker.EventFill:
		updated, err = service.applyFill(ctx, tx, account, current, event)
	default:
		return store.SecuritiesOrder{}, fmt.Errorf("apply broker event: %w: unknown event type", broker.ErrInvalidRequest)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.SecuritiesOrder{}, ErrInvalidOrderState
	}
	if err != nil {
		return store.SecuritiesOrder{}, fmt.Errorf("apply broker event %s: %w", event.Type, err)
	}
	action := orderEventName(event.Type)
	if event.Type == broker.EventFill && updated.Status == string(OrderPartiallyFilled) {
		action = "paper.order.partially_filled"
	}
	if event.Type == broker.EventOrderOpened && current.Status != string(OrderPendingSubmission) {
		action = "paper.order.replay_advanced"
	}
	if err := service.recordOrderMutation(ctx, tx, account, updated, action); err != nil {
		return store.SecuritiesOrder{}, err
	}
	return updated, nil
}

func (service *Service) applyFill(
	ctx context.Context,
	tx pgx.Tx,
	account store.SecuritiesPaperAccount,
	current store.SecuritiesOrder,
	event broker.Event,
) (store.SecuritiesOrder, error) {
	if !event.FillQuantity.IsPositive() || !event.FillPrice.IsPositive() {
		return store.SecuritiesOrder{}, broker.ErrInvalidRequest
	}
	newFilled, err := current.FilledQuantity.Add(event.FillQuantity)
	if err != nil || newFilled.Compare(current.Quantity) > 0 {
		return store.SecuritiesOrder{}, broker.ErrInvalidRequest
	}
	consideration, err := event.FillQuantity.Multiply(event.FillPrice, notionalPolicy)
	if err != nil || !consideration.IsPositive() {
		return store.SecuritiesOrder{}, fmt.Errorf("calculate fill consideration: %w", err)
	}
	if broker.Side(current.Side) == broker.SideBuy {
		if err := service.ensureBuyingPower(ctx, tx, account, consideration); err != nil {
			return store.SecuritiesOrder{}, err
		}
	}
	ledgerAccounts, err := service.ensureInstrumentLedgerAccounts(ctx, tx, account, current)
	if err != nil {
		return store.SecuritiesOrder{}, err
	}
	entries, err := service.fillEntries(ctx, tx, account, current, ledgerAccounts, event.FillQuantity, consideration)
	if err != nil {
		return store.SecuritiesOrder{}, err
	}
	if _, err := ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope:           "paper.fill",
		IdempotencyKey:  "fill." + event.ExternalEventID,
		TransactionType: "PAPER_SECURITY_FILL",
		PolicyVersion:   PolicyVersion,
		EffectiveAt:     event.OccurredAt.UTC(),
		Actor:           ledger.Actor{Type: "PROVIDER", ID: event.Provider},
		Entries:         entries,
		ProviderEvent: &ledger.ProviderEvent{
			Provider:        event.Provider,
			ExternalEventID: event.ExternalEventID,
			Payload:         event.Payload,
		},
	}); err != nil {
		return store.SecuritiesOrder{}, fmt.Errorf("post fill to ledger: %w", err)
	}
	if err := service.updatePosition(ctx, tx, account, current, event.FillQuantity, event.FillPrice, consideration); err != nil {
		return store.SecuritiesOrder{}, err
	}
	if _, err := store.New(tx).InsertFill(ctx, store.InsertFillParams{
		OrderID:         current.ID,
		Provider:        event.Provider,
		ExternalEventID: event.ExternalEventID,
		FillSequence:    event.FillSequence,
		Quantity:        event.FillQuantity,
		Price:           event.FillPrice,
		Consideration:   consideration,
		OccurredAt:      pgtype.Timestamptz{Time: event.OccurredAt.UTC(), Valid: true},
	}); err != nil {
		return store.SecuritiesOrder{}, fmt.Errorf("insert fill: %w", err)
	}

	previousNotional := money.Zero()
	if current.FilledQuantity.IsPositive() {
		previousAverage, err := decimalFromNumeric(current.AverageFillPrice)
		if err != nil {
			return store.SecuritiesOrder{}, err
		}
		previousNotional, err = current.FilledQuantity.Multiply(previousAverage, averagePolicy)
		if err != nil {
			return store.SecuritiesOrder{}, err
		}
	}
	totalNotional, err := previousNotional.Add(consideration)
	if err != nil {
		return store.SecuritiesOrder{}, err
	}
	average, err := totalNotional.Divide(newFilled, averagePolicy)
	if err != nil {
		return store.SecuritiesOrder{}, err
	}
	averageNumeric, err := average.NumericValue()
	if err != nil {
		return store.SecuritiesOrder{}, err
	}
	status := OrderPartiallyFilled
	if newFilled.Equal(current.Quantity) {
		status = OrderFilled
	}
	updated, err := store.New(tx).ApplyOrderFill(ctx, store.ApplyOrderFillParams{
		Status:           string(status),
		FilledQuantity:   newFilled,
		AverageFillPrice: averageNumeric,
		ReplayCursor:     event.ReplayCursor,
		ID:               current.ID,
	})
	if err != nil {
		return store.SecuritiesOrder{}, err
	}
	return updated, nil
}

func (service *Service) ensureInstrumentLedgerAccounts(
	ctx context.Context,
	tx pgx.Tx,
	account store.SecuritiesPaperAccount,
	order store.SecuritiesOrder,
) (store.SecuritiesInstrumentLedgerAccount, error) {
	queries := store.New(tx)
	existing, err := queries.GetInstrumentLedgerAccounts(ctx, store.GetInstrumentLedgerAccountsParams{
		PaperAccountID: account.ID,
		InstrumentID:   order.InstrumentID,
	})
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.SecuritiesInstrumentLedgerAccount{}, fmt.Errorf("get instrument ledger accounts: %w", err)
	}
	actor := ledger.Actor{Type: "SYSTEM", ID: "paper-securities"}
	ledgerService := ledger.NewService(tx)
	customer, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey:  "paper.security." + account.ID.String() + "." + strings.ToLower(order.Symbol),
		OwnerType:   "USER",
		OwnerID:     account.ID.String(),
		AccountType: "SECURITIES",
		Currency:    money.Currency(order.Symbol),
		NormalSide:  ledger.Debit,
		Actor:       actor,
	})
	if err != nil {
		return store.SecuritiesInstrumentLedgerAccount{}, fmt.Errorf("open customer security ledger: %w", err)
	}
	inventory, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey:  "paper.broker-inventory." + strings.ToLower(order.Symbol),
		OwnerType:   "PROVIDER",
		OwnerID:     service.broker.Name(),
		AccountType: "SECURITIES",
		Currency:    money.Currency(order.Symbol),
		NormalSide:  ledger.Credit,
		Actor:       actor,
	})
	if err != nil {
		return store.SecuritiesInstrumentLedgerAccount{}, fmt.Errorf("open broker inventory ledger: %w", err)
	}
	customerID, err := parseUUID(customer.ID)
	if err != nil {
		return store.SecuritiesInstrumentLedgerAccount{}, err
	}
	inventoryID, err := parseUUID(inventory.ID)
	if err != nil {
		return store.SecuritiesInstrumentLedgerAccount{}, err
	}
	created, err := queries.InsertInstrumentLedgerAccounts(ctx, store.InsertInstrumentLedgerAccountsParams{
		PaperAccountID:                 account.ID,
		InstrumentID:                   order.InstrumentID,
		Symbol:                         order.Symbol,
		CustomerLedgerAccountID:        customerID,
		BrokerInventoryLedgerAccountID: inventoryID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return queries.GetInstrumentLedgerAccounts(ctx, store.GetInstrumentLedgerAccountsParams{
			PaperAccountID: account.ID,
			InstrumentID:   order.InstrumentID,
		})
	}
	if err != nil {
		return store.SecuritiesInstrumentLedgerAccount{}, fmt.Errorf("insert instrument ledger accounts: %w", err)
	}
	return created, nil
}

func (service *Service) fillEntries(
	ctx context.Context,
	tx pgx.Tx,
	account store.SecuritiesPaperAccount,
	order store.SecuritiesOrder,
	securityAccounts store.SecuritiesInstrumentLedgerAccount,
	quantity money.Decimal,
	consideration money.Decimal,
) ([]ledger.Entry, error) {
	securityCurrency := money.Currency(order.Symbol)
	entries := make([]ledger.Entry, 0, 8)
	if broker.Side(order.Side) == broker.SideBuy {
		entries = append(entries,
			ledger.Entry{AccountID: securityAccounts.CustomerLedgerAccountID.String(), Currency: securityCurrency, Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: quantity},
			ledger.Entry{AccountID: securityAccounts.BrokerInventoryLedgerAccountID.String(), Currency: securityCurrency, Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: quantity},
		)
		ledgerService := ledger.NewService(tx)
		provisional, err := ledgerService.Balance(ctx, account.CashLedgerAccountID.String(), money.Currency("USD"), ledger.DimensionProvisionalBuying)
		if err != nil {
			return nil, err
		}
		fromProvisional := provisional
		if fromProvisional.Compare(consideration) > 0 {
			fromProvisional = consideration
		}
		if fromProvisional.IsPositive() {
			entries = append(entries,
				ledger.Entry{AccountID: account.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionProvisionalBuying, Direction: ledger.Credit, Amount: fromProvisional},
				ledger.Entry{AccountID: account.FundingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionProvisionalBuying, Direction: ledger.Debit, Amount: fromProvisional},
			)
		}
		fromSettled, err := consideration.Sub(fromProvisional)
		if err != nil {
			return nil, err
		}
		if fromSettled.IsPositive() {
			entries = append(entries,
				ledger.Entry{AccountID: account.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: fromSettled},
				ledger.Entry{AccountID: account.FundingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: fromSettled},
				ledger.Entry{AccountID: account.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: fromSettled},
				ledger.Entry{AccountID: account.FundingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: fromSettled},
			)
		}
		return entries, nil
	}
	entries = append(entries,
		ledger.Entry{AccountID: securityAccounts.CustomerLedgerAccountID.String(), Currency: securityCurrency, Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: quantity},
		ledger.Entry{AccountID: securityAccounts.BrokerInventoryLedgerAccountID.String(), Currency: securityCurrency, Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: quantity},
		ledger.Entry{AccountID: account.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionProvisionalBuying, Direction: ledger.Debit, Amount: consideration},
		ledger.Entry{AccountID: account.FundingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionProvisionalBuying, Direction: ledger.Credit, Amount: consideration},
	)
	return entries, nil
}

func orderEventName(eventType broker.EventType) string {
	switch eventType {
	case broker.EventOrderOpened:
		return "paper.order.opened"
	case broker.EventFill:
		return "paper.order.filled"
	case broker.EventOrderExpired:
		return "paper.order.expired"
	case broker.EventOrderRejected:
		return "paper.order.rejected"
	default:
		return "paper.order.updated"
	}
}

func (service *Service) recordOrderMutation(ctx context.Context, tx pgx.Tx, account store.SecuritiesPaperAccount, order store.SecuritiesOrder, action string) error {
	const maximumEventVersion = int64(1<<31 - 1)
	if order.Version <= 0 || order.Version > maximumEventVersion {
		return fmt.Errorf("record order mutation: invalid aggregate version %d", order.Version)
	}
	payload := []byte(fmt.Sprintf(`{"order_id":%q,"paper_account_id":%q,"status":%q,"symbol":%q}`, order.ID.String(), account.ID.String(), order.Status, order.Symbol))
	return recordMutation(ctx, tx, mutation{
		Action:        action,
		ResourceType:  "paper.order",
		ResourceID:    order.ID.String(),
		ActorType:     "USER",
		ActorID:       account.ID.String(),
		CorrelationID: order.ID,
		Metadata:      payload,
		AggregateType: "paper.order",
		AggregateID:   order.ID.String(),
		EventType:     action,
		EventVersion:  int32(order.Version),
		Payload:       payload,
	})
}
