package securities

import (
	"context"
	"errors"
	"fmt"

	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
	"github.com/KDTikkly/Cytisus/internal/securities/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) ListOrders(ctx context.Context, accessToken string, pageSize int32) ([]Order, error) {
	account, err := service.authenticate(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	queries := store.New(service.database)
	rows, err := queries.ListOrders(ctx, store.ListOrdersParams{PaperAccountID: account.ID, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	result := make([]Order, 0, len(rows))
	for _, row := range rows {
		fills, err := queries.ListFillsForOrder(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("list order fills: %w", err)
		}
		result = append(result, orderFromStore(row, fills))
	}
	return result, nil
}

func (service *Service) GetOrder(ctx context.Context, accessToken, orderID string) (Order, error) {
	account, err := service.authenticate(ctx, accessToken)
	if err != nil {
		return Order{}, err
	}
	identifier, err := parseUUID(orderID)
	if err != nil {
		return Order{}, ErrOrderNotFound
	}
	queries := store.New(service.database)
	row, err := queries.GetOrder(ctx, identifier)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && row.PaperAccountID != account.ID {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("get order: %w", err)
	}
	fills, err := queries.ListFillsForOrder(ctx, row.ID)
	if err != nil {
		return Order{}, fmt.Errorf("list order fills: %w", err)
	}
	return orderFromStore(row, fills), nil
}

func (service *Service) ListPositions(ctx context.Context, accessToken string) ([]Position, error) {
	account, err := service.authenticate(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(service.database).ListPositions(ctx, account.ID)
	if err != nil {
		return nil, fmt.Errorf("list positions: %w", err)
	}
	return service.positionsWithMarketData(ctx, rows)
}

func (service *Service) Portfolio(ctx context.Context, accessToken string) (Portfolio, error) {
	account, err := service.authenticate(ctx, accessToken)
	if err != nil {
		return Portfolio{}, err
	}
	ledgerService := ledger.NewService(service.database)
	settled, err := ledgerService.Balance(ctx, account.CashLedgerAccountID.String(), money.Currency("USD"), ledger.DimensionSettled)
	if err != nil {
		return Portfolio{}, err
	}
	withdrawable, err := ledgerService.Balance(ctx, account.CashLedgerAccountID.String(), money.Currency("USD"), ledger.DimensionWithdrawable)
	if err != nil {
		return Portfolio{}, err
	}
	provisional, err := ledgerService.Balance(ctx, account.CashLedgerAccountID.String(), money.Currency("USD"), ledger.DimensionProvisionalBuying)
	if err != nil {
		return Portfolio{}, err
	}
	total, err := settled.Add(provisional)
	if err != nil {
		return Portfolio{}, fmt.Errorf("calculate total buying power: %w", err)
	}
	rows, err := store.New(service.database).ListPositions(ctx, account.ID)
	if err != nil {
		return Portfolio{}, fmt.Errorf("list portfolio positions: %w", err)
	}
	positions, err := service.positionsWithMarketData(ctx, rows)
	if err != nil {
		return Portfolio{}, err
	}
	return Portfolio{
		Cash: CashSummary{
			Settled:                settled,
			Withdrawable:           withdrawable,
			ProvisionalBuyingPower: provisional,
			TotalBuyingPower:       total,
		},
		Positions: positions,
	}, nil
}

func (service *Service) positionsWithMarketData(ctx context.Context, rows []store.SecuritiesPosition) ([]Position, error) {
	positions := make([]Position, 0, len(rows))
	for _, row := range rows {
		quote, err := service.Quote(ctx, row.Symbol, 0)
		if err != nil {
			return nil, fmt.Errorf("quote portfolio position %s: %w", row.Symbol, err)
		}
		marketValue, err := row.Quantity.Multiply(quote.Last, notionalPolicy)
		if err != nil {
			return nil, fmt.Errorf("value portfolio position %s: %w", row.Symbol, err)
		}
		positions = append(positions, Position{
			Symbol:      row.Symbol,
			Quantity:    row.Quantity,
			AverageCost: row.AverageCost,
			CostBasis:   row.CostBasis,
			RealizedPnL: row.RealizedPnl,
			MarketPrice: quote.Last,
			MarketValue: marketValue,
			QuoteStatus: quote.Status,
			UpdatedAt:   row.UpdatedAt.Time.UTC(),
		})
	}
	return positions, nil
}

func (service *Service) updatePosition(
	ctx context.Context,
	tx pgx.Tx,
	account store.SecuritiesPaperAccount,
	order store.SecuritiesOrder,
	fillQuantity money.Decimal,
	fillPrice money.Decimal,
	consideration money.Decimal,
) error {
	queries := store.New(tx)
	current, err := queries.GetPositionForUpdate(ctx, store.GetPositionForUpdateParams{
		PaperAccountID: account.ID,
		InstrumentID:   order.InstrumentID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		current = store.SecuritiesPosition{
			PaperAccountID: account.ID,
			InstrumentID:   order.InstrumentID,
			Symbol:         order.Symbol,
		}
	} else if err != nil {
		return fmt.Errorf("lock position: %w", err)
	}
	quantity := current.Quantity
	averageCost := current.AverageCost
	costBasis := current.CostBasis
	realizedPnL := current.RealizedPnl
	if broker.Side(order.Side) == broker.SideBuy {
		quantity, err = quantity.Add(fillQuantity)
		if err != nil {
			return err
		}
		costBasis, err = costBasis.Add(consideration)
		if err != nil {
			return err
		}
		averageCost, err = costBasis.Divide(quantity, averagePolicy)
		if err != nil {
			return err
		}
	} else {
		if quantity.Compare(fillQuantity) < 0 {
			return ErrInsufficientPosition
		}
		quantity, err = quantity.Sub(fillQuantity)
		if err != nil {
			return err
		}
		removedCost, multiplyErr := current.AverageCost.Multiply(fillQuantity, averagePolicy)
		if multiplyErr != nil {
			return multiplyErr
		}
		costBasis, err = costBasis.Sub(removedCost)
		if err != nil {
			return err
		}
		profit, subtractErr := consideration.Sub(removedCost)
		if subtractErr != nil {
			return subtractErr
		}
		realizedPnL, err = realizedPnL.Add(profit)
		if err != nil {
			return err
		}
		if quantity.IsZero() {
			averageCost = money.Zero()
			costBasis = money.Zero()
		}
	}
	if _, err := queries.UpsertPosition(ctx, store.UpsertPositionParams{
		PaperAccountID: account.ID,
		InstrumentID:   order.InstrumentID,
		Symbol:         order.Symbol,
		Quantity:       quantity,
		AverageCost:    averageCost,
		CostBasis:      costBasis,
		RealizedPnl:    realizedPnL,
	}); err != nil {
		return fmt.Errorf("update position projection: %w", err)
	}
	_ = fillPrice
	return nil
}

func (service *Service) orderWithFills(ctx context.Context, order store.SecuritiesOrder) (Order, error) {
	fills, err := store.New(service.database).ListFillsForOrder(ctx, order.ID)
	if err != nil {
		return Order{}, fmt.Errorf("list order fills: %w", err)
	}
	return orderFromStore(order, fills), nil
}

func orderFromStore(order store.SecuritiesOrder, rows []store.SecuritiesFill) Order {
	limit, _ := decimalFromNullableNumeric(order.LimitPrice)
	average, _ := decimalFromNullableNumeric(order.AverageFillPrice)
	fills := make([]Fill, 0, len(rows))
	for _, row := range rows {
		fills = append(fills, Fill{
			ID:              row.ID.String(),
			ExternalEventID: row.ExternalEventID,
			Sequence:        row.FillSequence,
			Quantity:        row.Quantity,
			Price:           row.Price,
			Consideration:   row.Consideration,
			OccurredAt:      row.OccurredAt.Time.UTC(),
		})
	}
	return Order{
		ID:                  order.ID.String(),
		Symbol:              order.Symbol,
		Side:                broker.Side(order.Side),
		OrderType:           broker.OrderType(order.OrderType),
		TimeInForce:         broker.TimeInForce(order.TimeInForce),
		Quantity:            order.Quantity,
		LimitPrice:          limit,
		Status:              OrderStatus(order.Status),
		RejectionCode:       order.RejectionCode.String,
		FilledQuantity:      order.FilledQuantity,
		AverageFillPrice:    average,
		ReferencePrice:      order.ReferencePrice,
		QuoteStatus:         marketdata.QuoteStatus(order.QuoteStatus),
		QuoteObservedAt:     order.QuoteObservedAt.Time.UTC(),
		ReplayCursor:        order.ReplayCursor,
		ProviderOrderID:     order.ProviderOrderID,
		DeterministicReplay: true,
		CreatedAt:           order.CreatedAt.Time.UTC(),
		UpdatedAt:           order.UpdatedAt.Time.UTC(),
		Fills:               fills,
	}
}

func nullableNumeric(value *money.Decimal) (pgtype.Numeric, error) {
	if value == nil {
		return pgtype.Numeric{}, nil
	}
	return value.NumericValue()
}

func decimalFromNullableNumeric(value pgtype.Numeric) (*money.Decimal, error) {
	if !value.Valid {
		return nil, nil
	}
	decimal, err := decimalFromNumeric(value)
	if err != nil {
		return nil, err
	}
	return &decimal, nil
}

func decimalFromNumeric(value pgtype.Numeric) (money.Decimal, error) {
	var decimal money.Decimal
	if err := decimal.ScanNumeric(value); err != nil {
		return money.Decimal{}, fmt.Errorf("decode decimal: %w", err)
	}
	return decimal, nil
}
