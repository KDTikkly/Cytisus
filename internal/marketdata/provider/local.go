package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/marketdata/store"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const LocalMarketDataProvider = "local-market-simulator"

type Local struct {
	queries *store.Queries
}

func NewLocal(database store.DBTX, environment config.Environment) (*Local, error) {
	if database == nil || (environment != config.EnvironmentLocal && environment != config.EnvironmentTest) {
		return nil, marketdata.ErrSimulatorDisabled
	}
	return &Local{queries: store.New(database)}, nil
}

func (provider *Local) Name() string {
	return LocalMarketDataProvider
}

func (provider *Local) Capabilities(context.Context) marketdata.ProviderCapabilities {
	return marketdata.ProviderCapabilities{
		Mode:                marketdata.QuoteStatusSimulated,
		DeterministicReplay: true,
		MaximumReplayCursor: 2,
	}
}

func (provider *Local) Health(ctx context.Context) error {
	if provider == nil || provider.queries == nil {
		return marketdata.ErrSimulatorDisabled
	}
	count, err := provider.queries.CountInstrumentFixtures(ctx)
	if err != nil {
		return fmt.Errorf("check local market data health: %w", err)
	}
	if count == 0 {
		return marketdata.ErrQuoteUnavailable
	}
	return nil
}

func (provider *Local) Quote(ctx context.Context, symbol string, replayCursor int32) (marketdata.Quote, error) {
	if provider == nil || provider.queries == nil || replayCursor < 0 {
		return marketdata.Quote{}, marketdata.ErrQuoteUnavailable
	}
	row, err := provider.queries.GetQuoteFixture(ctx, store.GetQuoteFixtureParams{
		Symbol:       strings.ToUpper(strings.TrimSpace(symbol)),
		ReplayCursor: replayCursor,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return marketdata.Quote{}, marketdata.ErrQuoteUnavailable
	}
	if err != nil {
		return marketdata.Quote{}, fmt.Errorf("get deterministic quote: %w", err)
	}
	bid, err := decimalFromNumeric(row.Bid)
	if err != nil {
		return marketdata.Quote{}, marketdata.ErrQuoteUnavailable
	}
	ask, err := decimalFromNumeric(row.Ask)
	if err != nil {
		return marketdata.Quote{}, marketdata.ErrQuoteUnavailable
	}
	last, err := decimalFromNumeric(row.Last)
	if err != nil {
		return marketdata.Quote{}, marketdata.ErrQuoteUnavailable
	}
	return marketdata.Quote{
		InstrumentID: row.InstrumentID.String(),
		Symbol:       row.Symbol,
		ReplayCursor: row.ReplayCursor,
		Bid:          bid,
		Ask:          ask,
		Last:         last,
		Status:       marketdata.QuoteStatus(row.Status),
		MarketStatus: marketdata.MarketStatus(row.MarketStatus),
		ObservedAt:   row.ObservedAt.Time,
		Provider:     row.Provider,
	}, nil
}

func decimalFromNumeric(value pgtype.Numeric) (money.Decimal, error) {
	var decimal money.Decimal
	if err := decimal.ScanNumeric(value); err != nil {
		return money.Decimal{}, err
	}
	return decimal, nil
}
