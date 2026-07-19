package marketdata

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/marketdata/store"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
)

type Catalog struct {
	queries *store.Queries
}

func NewCatalog(database store.DBTX) *Catalog {
	return &Catalog{queries: store.New(database)}
}

func (catalog *Catalog) Search(ctx context.Context, query string, pageSize int32) ([]Instrument, error) {
	if catalog == nil || catalog.queries == nil {
		return nil, fmt.Errorf("search instruments: catalog is required")
	}
	if pageSize <= 0 || pageSize > 50 {
		pageSize = 20
	}
	trimmed := strings.TrimSpace(query)
	pattern := "%" + escapeLike(trimmed) + "%"
	rows, err := catalog.queries.SearchInstruments(ctx, store.SearchInstrumentsParams{
		QueryPattern: pattern,
		ExactSymbol:  strings.ToUpper(trimmed),
		PageSize:     pageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("search instruments: %w", err)
	}
	result := make([]Instrument, 0, len(rows))
	for _, row := range rows {
		result = append(result, instrumentFromSearchRow(row))
	}
	return result, nil
}

func (catalog *Catalog) GetBySymbol(ctx context.Context, symbol string) (Instrument, error) {
	if catalog == nil || catalog.queries == nil {
		return Instrument{}, fmt.Errorf("get instrument: catalog is required")
	}
	row, err := catalog.queries.GetInstrumentBySymbol(ctx, strings.ToUpper(strings.TrimSpace(symbol)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Instrument{}, ErrInstrumentNotFound
	}
	if err != nil {
		return Instrument{}, fmt.Errorf("get instrument: %w", err)
	}
	return instrumentFromGetRow(row), nil
}

func instrumentFromSearchRow(row store.SearchInstrumentsRow) Instrument {
	return Instrument{
		ID:              row.ID.String(),
		Symbol:          row.Symbol,
		DisplayName:     row.DisplayName,
		AssetType:       AssetType(row.AssetType),
		PrimaryExchange: row.PrimaryExchange,
		Currency:        money.Currency(row.Currency),
		Listed:          row.Listed,
		Capability: Capability{
			Searchable:         row.Searchable,
			QuoteEnabled:       row.QuoteEnabled,
			PaperTradable:      row.PaperTradable,
			LiveTradable:       row.LiveTradable,
			FractionalEnabled:  row.FractionalEnabled,
			TransferOutEnabled: row.TransferOutEnabled,
			RWAMintEnabled:     row.RwaMintEnabled,
			UserEligible:       row.UserEligible,
			DisabledReason:     row.DisabledReason.String,
			PolicyVersion:      row.PolicyVersion,
		},
	}
}

func instrumentFromGetRow(row store.GetInstrumentBySymbolRow) Instrument {
	return Instrument{
		ID:              row.ID.String(),
		Symbol:          row.Symbol,
		DisplayName:     row.DisplayName,
		AssetType:       AssetType(row.AssetType),
		PrimaryExchange: row.PrimaryExchange,
		Currency:        money.Currency(row.Currency),
		Listed:          row.Listed,
		Capability: Capability{
			Searchable:         row.Searchable,
			QuoteEnabled:       row.QuoteEnabled,
			PaperTradable:      row.PaperTradable,
			LiveTradable:       row.LiveTradable,
			FractionalEnabled:  row.FractionalEnabled,
			TransferOutEnabled: row.TransferOutEnabled,
			RWAMintEnabled:     row.RwaMintEnabled,
			UserEligible:       row.UserEligible,
			DisabledReason:     row.DisabledReason.String,
			PolicyVersion:      row.PolicyVersion,
		},
	}
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
