package paperapi

import (
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities"
)

func accountDTO(account securities.PaperAccount) map[string]any {
	return map[string]any{
		"id":                 account.ID,
		"fixture_id":         account.FixtureID,
		"customer_reference": account.CustomerReference,
		"initial_cash":       account.InitialCash.String(),
		"created_at":         account.CreatedAt.Format(timeFormat),
	}
}

func instrumentDTO(instrument marketdata.Instrument) map[string]any {
	return map[string]any{
		"id":               instrument.ID,
		"symbol":           instrument.Symbol,
		"display_name":     instrument.DisplayName,
		"asset_type":       instrument.AssetType,
		"primary_exchange": instrument.PrimaryExchange,
		"currency":         instrument.Currency,
		"listed":           instrument.Listed,
		"capability": map[string]any{
			"searchable":           instrument.Capability.Searchable,
			"quote_enabled":        instrument.Capability.QuoteEnabled,
			"paper_tradable":       instrument.Capability.PaperTradable,
			"live_tradable":        instrument.Capability.LiveTradable,
			"fractional_enabled":   instrument.Capability.FractionalEnabled,
			"transfer_out_enabled": instrument.Capability.TransferOutEnabled,
			"rwa_mint_enabled":     instrument.Capability.RWAMintEnabled,
			"user_eligible":        instrument.Capability.UserEligible,
			"disabled_reason":      nullableString(instrument.Capability.DisabledReason),
			"policy_version":       instrument.Capability.PolicyVersion,
		},
	}
}

func quoteDTO(quote marketdata.Quote) map[string]any {
	return map[string]any{
		"instrument_id": quote.InstrumentID,
		"symbol":        quote.Symbol,
		"replay_cursor": quote.ReplayCursor,
		"bid":           decimalOrNull(quote.Bid),
		"ask":           decimalOrNull(quote.Ask),
		"last":          decimalOrNull(quote.Last),
		"status":        quote.Status,
		"market_status": quote.MarketStatus,
		"observed_at":   quote.ObservedAt.Format(timeFormat),
		"provider":      quote.Provider,
	}
}

func orderDTO(order securities.Order) map[string]any {
	fills := make([]any, 0, len(order.Fills))
	for _, fill := range order.Fills {
		fills = append(fills, map[string]any{
			"id":                fill.ID,
			"external_event_id": fill.ExternalEventID,
			"sequence":          fill.Sequence,
			"quantity":          fill.Quantity.String(),
			"price":             fill.Price.String(),
			"consideration":     fill.Consideration.String(),
			"occurred_at":       fill.OccurredAt.Format(timeFormat),
		})
	}
	return map[string]any{
		"id":                   order.ID,
		"symbol":               order.Symbol,
		"side":                 order.Side,
		"order_type":           order.OrderType,
		"time_in_force":        order.TimeInForce,
		"quantity":             order.Quantity.String(),
		"limit_price":          decimalPointer(order.LimitPrice),
		"status":               order.Status,
		"rejection_code":       nullableString(order.RejectionCode),
		"filled_quantity":      order.FilledQuantity.String(),
		"average_fill_price":   decimalPointer(order.AverageFillPrice),
		"reference_price":      order.ReferencePrice.String(),
		"quote_status":         order.QuoteStatus,
		"quote_observed_at":    order.QuoteObservedAt.Format(timeFormat),
		"replay_cursor":        order.ReplayCursor,
		"provider_order_id":    order.ProviderOrderID,
		"deterministic_replay": order.DeterministicReplay,
		"created_at":           order.CreatedAt.Format(timeFormat),
		"updated_at":           order.UpdatedAt.Format(timeFormat),
		"fills":                fills,
	}
}

func positionDTO(position securities.Position) map[string]any {
	return map[string]any{
		"symbol":       position.Symbol,
		"quantity":     position.Quantity.String(),
		"average_cost": position.AverageCost.String(),
		"cost_basis":   position.CostBasis.String(),
		"realized_pnl": position.RealizedPnL.String(),
		"market_price": position.MarketPrice.String(),
		"market_value": position.MarketValue.String(),
		"quote_status": position.QuoteStatus,
		"updated_at":   position.UpdatedAt.Format(timeFormat),
	}
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func decimalOrNull(value money.Decimal) any {
	if value.IsZero() {
		return nil
	}
	return value.String()
}

func decimalPointer(value *money.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}
