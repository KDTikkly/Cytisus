-- name: SearchInstruments :many
SELECT
    instrument.id,
    instrument.symbol,
    instrument.display_name,
    instrument.asset_type,
    instrument.primary_exchange,
    instrument.currency,
    instrument.listed,
    capability.searchable,
    capability.quote_enabled,
    capability.paper_tradable,
    capability.live_tradable,
    capability.fractional_enabled,
    capability.transfer_out_enabled,
    capability.rwa_mint_enabled,
    capability.user_eligible,
    capability.disabled_reason,
    capability.policy_version
FROM marketdata.instruments AS instrument
JOIN marketdata.instrument_capabilities AS capability
    ON capability.instrument_id = instrument.id
WHERE capability.searchable
  AND (
      instrument.symbol ILIKE sqlc.arg(query_pattern)::text ESCAPE '\'
      OR instrument.display_name ILIKE sqlc.arg(query_pattern)::text ESCAPE '\'
  )
ORDER BY
    CASE WHEN instrument.symbol = sqlc.arg(exact_symbol) THEN 0 ELSE 1 END,
    instrument.symbol
LIMIT sqlc.arg(page_size);

-- name: GetInstrumentBySymbol :one
SELECT
    instrument.id,
    instrument.symbol,
    instrument.display_name,
    instrument.asset_type,
    instrument.primary_exchange,
    instrument.currency,
    instrument.listed,
    capability.searchable,
    capability.quote_enabled,
    capability.paper_tradable,
    capability.live_tradable,
    capability.fractional_enabled,
    capability.transfer_out_enabled,
    capability.rwa_mint_enabled,
    capability.user_eligible,
    capability.disabled_reason,
    capability.policy_version
FROM marketdata.instruments AS instrument
JOIN marketdata.instrument_capabilities AS capability
    ON capability.instrument_id = instrument.id
WHERE instrument.symbol = sqlc.arg(symbol);

-- name: GetQuoteFixture :one
SELECT
    instrument.id AS instrument_id,
    instrument.symbol,
    fixture.replay_cursor,
    fixture.bid,
    fixture.ask,
    fixture.last,
    fixture.status,
    fixture.market_status,
    fixture.observed_at,
    fixture.provider
FROM marketdata.quote_fixtures AS fixture
JOIN marketdata.instruments AS instrument
    ON instrument.id = fixture.instrument_id
JOIN marketdata.instrument_capabilities AS capability
    ON capability.instrument_id = instrument.id
WHERE instrument.symbol = sqlc.arg(symbol)
  AND fixture.replay_cursor = sqlc.arg(replay_cursor)
  AND capability.quote_enabled;

-- name: CountInstrumentFixtures :one
SELECT COUNT(*)
FROM marketdata.instruments
WHERE fixture;
