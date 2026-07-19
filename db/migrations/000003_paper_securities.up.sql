CREATE SCHEMA marketdata;
CREATE SCHEMA securities;

CREATE FUNCTION marketdata.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE marketdata.instruments (
    id UUID PRIMARY KEY,
    symbol TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    asset_type TEXT NOT NULL,
    primary_exchange TEXT NOT NULL,
    currency VARCHAR(12) NOT NULL DEFAULT 'USD',
    listed BOOLEAN NOT NULL DEFAULT TRUE,
    fixture BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT marketdata_instruments_symbol_check
        CHECK (symbol ~ '^[A-Z][A-Z0-9.-]{0,15}$'),
    CONSTRAINT marketdata_instruments_name_check
        CHECK (display_name <> ''),
    CONSTRAINT marketdata_instruments_type_check
        CHECK (asset_type IN (
            'COMMON_STOCK',
            'ETF',
            'ADR',
            'REIT',
            'PREFERRED',
            'CLOSED_END_FUND',
            'ETN',
            'WARRANT',
            'UNIT',
            'RIGHT',
            'OTHER'
        )),
    CONSTRAINT marketdata_instruments_exchange_check
        CHECK (primary_exchange <> ''),
    CONSTRAINT marketdata_instruments_currency_check
        CHECK (currency ~ '^[A-Z][A-Z0-9]{2,11}$')
);

CREATE TABLE marketdata.instrument_capabilities (
    instrument_id UUID PRIMARY KEY REFERENCES marketdata.instruments(id) ON DELETE RESTRICT,
    searchable BOOLEAN NOT NULL,
    quote_enabled BOOLEAN NOT NULL,
    paper_tradable BOOLEAN NOT NULL,
    live_tradable BOOLEAN NOT NULL,
    fractional_enabled BOOLEAN NOT NULL,
    transfer_out_enabled BOOLEAN NOT NULL,
    rwa_mint_enabled BOOLEAN NOT NULL,
    user_eligible BOOLEAN NOT NULL,
    disabled_reason TEXT,
    policy_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT marketdata_capabilities_disabled_reason_check
        CHECK (
            (paper_tradable AND disabled_reason IS NULL)
            OR (NOT paper_tradable AND disabled_reason ~ '^[A-Z][A-Z0-9_]{2,63}$')
        ),
    CONSTRAINT marketdata_capabilities_policy_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$')
);

CREATE FUNCTION marketdata.enforce_paper_tradable_type()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_asset_type TEXT;
BEGIN
    SELECT instrument.asset_type
    INTO STRICT target_asset_type
    FROM marketdata.instruments AS instrument
    WHERE instrument.id = NEW.instrument_id;

    IF NEW.paper_tradable AND target_asset_type NOT IN ('COMMON_STOCK', 'ETF') THEN
        RAISE EXCEPTION 'asset type % is view-only for paper trading', target_asset_type
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER marketdata_capabilities_paper_type
BEFORE INSERT OR UPDATE ON marketdata.instrument_capabilities
FOR EACH ROW EXECUTE FUNCTION marketdata.enforce_paper_tradable_type();

CREATE TABLE marketdata.instrument_symbol_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id UUID NOT NULL REFERENCES marketdata.instruments(id) ON DELETE RESTRICT,
    old_symbol TEXT NOT NULL,
    new_symbol TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    reason_code TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT marketdata_symbol_history_symbols_check
        CHECK (old_symbol <> new_symbol),
    CONSTRAINT marketdata_symbol_history_reason_check
        CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    UNIQUE (instrument_id, effective_at)
);

CREATE TRIGGER marketdata_symbol_history_immutable
BEFORE UPDATE OR DELETE ON marketdata.instrument_symbol_history
FOR EACH ROW EXECUTE FUNCTION marketdata.reject_immutable_mutation();

CREATE TABLE marketdata.quote_fixtures (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id UUID NOT NULL REFERENCES marketdata.instruments(id) ON DELETE RESTRICT,
    replay_cursor INTEGER NOT NULL,
    bid NUMERIC(38, 18),
    ask NUMERIC(38, 18),
    last NUMERIC(38, 18),
    status TEXT NOT NULL,
    market_status TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL DEFAULT 'local-market-simulator',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT marketdata_quote_cursor_check
        CHECK (replay_cursor >= 0),
    CONSTRAINT marketdata_quote_price_check
        CHECK (
            (bid IS NULL OR bid > 0)
            AND (ask IS NULL OR ask > 0)
            AND (last IS NULL OR last > 0)
            AND (bid IS NULL OR ask IS NULL OR bid <= ask)
        ),
    CONSTRAINT marketdata_quote_status_check
        CHECK (status IN ('SIMULATED', 'STALE', 'UNAVAILABLE')),
    CONSTRAINT marketdata_quote_market_status_check
        CHECK (market_status IN ('PRE_MARKET', 'OPEN', 'AFTER_HOURS', 'CLOSED', 'HALTED')),
    CONSTRAINT marketdata_quote_provider_check
        CHECK (provider = 'local-market-simulator'),
    UNIQUE (instrument_id, replay_cursor)
);

CREATE INDEX marketdata_instruments_search_idx
    ON marketdata.instruments (symbol, display_name);

CREATE TRIGGER marketdata_quote_fixtures_immutable
BEFORE UPDATE OR DELETE ON marketdata.quote_fixtures
FOR EACH ROW EXECUTE FUNCTION marketdata.reject_immutable_mutation();

CREATE FUNCTION securities.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE securities.paper_accounts (
    id UUID PRIMARY KEY,
    fixture_id TEXT NOT NULL UNIQUE,
    customer_reference TEXT NOT NULL UNIQUE,
    session_token_hash CHAR(64) NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    cash_ledger_account_id UUID NOT NULL,
    funding_ledger_account_id UUID NOT NULL,
    initial_cash NUMERIC(38, 18) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT securities_paper_accounts_fixture_check
        CHECK (fixture_id ~ '^[a-z][a-z0-9._:-]{2,127}$'),
    CONSTRAINT securities_paper_accounts_customer_check
        CHECK (customer_reference <> ''),
    CONSTRAINT securities_paper_accounts_token_check
        CHECK (session_token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT securities_paper_accounts_status_check
        CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    CONSTRAINT securities_paper_accounts_cash_check
        CHECK (initial_cash > 0),
    CONSTRAINT securities_paper_accounts_archive_check
        CHECK ((status = 'ARCHIVED') = (archived_at IS NOT NULL))
);

CREATE TABLE securities.instrument_ledger_accounts (
    paper_account_id UUID NOT NULL REFERENCES securities.paper_accounts(id) ON DELETE RESTRICT,
    instrument_id UUID NOT NULL,
    symbol TEXT NOT NULL,
    customer_ledger_account_id UUID NOT NULL,
    broker_inventory_ledger_account_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (paper_account_id, instrument_id),
    CONSTRAINT securities_instrument_ledger_symbol_check
        CHECK (symbol ~ '^[A-Z][A-Z0-9.-]{0,15}$')
);

CREATE TABLE securities.orders (
    id UUID PRIMARY KEY,
    paper_account_id UUID NOT NULL REFERENCES securities.paper_accounts(id) ON DELETE RESTRICT,
    instrument_id UUID NOT NULL,
    symbol TEXT NOT NULL,
    client_order_reference CHAR(64) NOT NULL,
    side TEXT NOT NULL,
    order_type TEXT NOT NULL,
    time_in_force TEXT NOT NULL,
    quantity NUMERIC(38, 18) NOT NULL,
    limit_price NUMERIC(38, 18),
    status TEXT NOT NULL,
    rejection_code TEXT,
    filled_quantity NUMERIC(38, 18) NOT NULL DEFAULT 0,
    average_fill_price NUMERIC(38, 18),
    reference_price NUMERIC(38, 18) NOT NULL,
    quote_status TEXT NOT NULL,
    quote_observed_at TIMESTAMPTZ NOT NULL,
    replay_cursor INTEGER NOT NULL DEFAULT 0,
    provider_order_id TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT securities_orders_symbol_check
        CHECK (symbol ~ '^[A-Z][A-Z0-9.-]{0,15}$'),
    CONSTRAINT securities_orders_client_reference_check
        CHECK (client_order_reference ~ '^[0-9a-f]{64}$'),
    CONSTRAINT securities_orders_side_check
        CHECK (side IN ('BUY', 'SELL')),
    CONSTRAINT securities_orders_type_check
        CHECK (order_type IN ('MARKET', 'LIMIT')),
    CONSTRAINT securities_orders_tif_check
        CHECK (time_in_force IN ('DAY', 'GTC')),
    CONSTRAINT securities_orders_quantity_check
        CHECK (quantity > 0 AND filled_quantity >= 0 AND filled_quantity <= quantity),
    CONSTRAINT securities_orders_limit_check
        CHECK (
            (order_type = 'MARKET' AND limit_price IS NULL)
            OR (order_type = 'LIMIT' AND limit_price > 0)
        ),
    CONSTRAINT securities_orders_status_check
        CHECK (status IN (
            'PENDING_SUBMISSION',
            'OPEN',
            'PARTIALLY_FILLED',
            'FILLED',
            'CANCELLED',
            'REJECTED',
            'EXPIRED'
        )),
    CONSTRAINT securities_orders_rejection_check
        CHECK ((status = 'REJECTED') = (rejection_code IS NOT NULL)),
    CONSTRAINT securities_orders_average_fill_check
        CHECK (
            (filled_quantity = 0 AND average_fill_price IS NULL)
            OR (filled_quantity > 0 AND average_fill_price > 0)
        ),
    CONSTRAINT securities_orders_quote_status_check
        CHECK (quote_status IN ('SIMULATED', 'STALE')),
    CONSTRAINT securities_orders_replay_cursor_check
        CHECK (replay_cursor >= 0),
    CONSTRAINT securities_orders_policy_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT securities_orders_version_check
        CHECK (version > 0),
    UNIQUE (paper_account_id, client_order_reference),
    UNIQUE (provider_order_id)
);

CREATE TABLE securities.order_requests (
    paper_account_id UUID NOT NULL REFERENCES securities.paper_accounts(id) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    order_id UUID NOT NULL UNIQUE REFERENCES securities.orders(id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (paper_account_id, idempotency_key),
    CONSTRAINT securities_order_requests_key_check
        CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT securities_order_requests_hash_check
        CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE securities.broker_events (
    provider TEXT NOT NULL,
    external_event_id TEXT NOT NULL,
    order_id UUID NOT NULL REFERENCES securities.orders(id) ON DELETE RESTRICT,
    payload_hash CHAR(64) NOT NULL,
    event_type TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (provider, external_event_id),
    CONSTRAINT securities_broker_events_provider_check
        CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT securities_broker_events_external_id_check
        CHECK (external_event_id <> '' AND length(external_event_id) <= 255),
    CONSTRAINT securities_broker_events_payload_hash_check
        CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT securities_broker_events_type_check
        CHECK (event_type IN ('ORDER_OPENED', 'ORDER_REJECTED', 'FILL', 'ORDER_EXPIRED'))
);

CREATE TABLE securities.fills (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES securities.orders(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    external_event_id TEXT NOT NULL,
    fill_sequence INTEGER NOT NULL,
    quantity NUMERIC(38, 18) NOT NULL,
    price NUMERIC(38, 18) NOT NULL,
    consideration NUMERIC(38, 18) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT securities_fills_sequence_check
        CHECK (fill_sequence > 0),
    CONSTRAINT securities_fills_amount_check
        CHECK (quantity > 0 AND price > 0 AND consideration > 0),
    UNIQUE (provider, external_event_id),
    UNIQUE (order_id, fill_sequence)
);

CREATE TABLE securities.positions (
    paper_account_id UUID NOT NULL REFERENCES securities.paper_accounts(id) ON DELETE RESTRICT,
    instrument_id UUID NOT NULL,
    symbol TEXT NOT NULL,
    quantity NUMERIC(38, 18) NOT NULL DEFAULT 0,
    average_cost NUMERIC(38, 18) NOT NULL DEFAULT 0,
    cost_basis NUMERIC(38, 18) NOT NULL DEFAULT 0,
    realized_pnl NUMERIC(38, 18) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (paper_account_id, instrument_id),
    CONSTRAINT securities_positions_symbol_check
        CHECK (symbol ~ '^[A-Z][A-Z0-9.-]{0,15}$'),
    CONSTRAINT securities_positions_quantity_check
        CHECK (quantity >= 0),
    CONSTRAINT securities_positions_cost_check
        CHECK (average_cost >= 0 AND cost_basis >= 0),
    CONSTRAINT securities_positions_empty_check
        CHECK (
            quantity > 0
            OR (quantity = 0 AND average_cost = 0 AND cost_basis = 0)
        ),
    CONSTRAINT securities_positions_version_check
        CHECK (version > 0)
);

CREATE INDEX securities_orders_account_created_idx
    ON securities.orders (paper_account_id, created_at DESC, id DESC);

CREATE INDEX securities_fills_order_idx
    ON securities.fills (order_id, fill_sequence);

CREATE FUNCTION securities.enforce_order_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = NEW.status THEN
        IF OLD.status NOT IN ('OPEN', 'PARTIALLY_FILLED') OR NEW.replay_cursor <= OLD.replay_cursor THEN
            RAISE EXCEPTION 'order status transition must change state or advance replay'
                USING ERRCODE = '23514';
        END IF;
        NEW.updated_at := clock_timestamp();
        NEW.version := OLD.version + 1;
        RETURN NEW;
    END IF;
    IF NOT (
        (OLD.status = 'PENDING_SUBMISSION' AND NEW.status IN ('OPEN', 'PARTIALLY_FILLED', 'FILLED', 'REJECTED'))
        OR (OLD.status = 'OPEN' AND NEW.status IN ('PARTIALLY_FILLED', 'FILLED', 'CANCELLED', 'REJECTED', 'EXPIRED'))
        OR (OLD.status = 'PARTIALLY_FILLED' AND NEW.status IN ('FILLED', 'CANCELLED', 'EXPIRED'))
    ) THEN
        RAISE EXCEPTION 'invalid order status transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER securities_orders_state_transition
BEFORE UPDATE ON securities.orders
FOR EACH ROW EXECUTE FUNCTION securities.enforce_order_transition();

CREATE TRIGGER securities_order_requests_immutable
BEFORE UPDATE OR DELETE ON securities.order_requests
FOR EACH ROW EXECUTE FUNCTION securities.reject_immutable_mutation();

CREATE TRIGGER securities_broker_events_immutable
BEFORE UPDATE OR DELETE ON securities.broker_events
FOR EACH ROW EXECUTE FUNCTION securities.reject_immutable_mutation();

CREATE TRIGGER securities_fills_immutable
BEFORE UPDATE OR DELETE ON securities.fills
FOR EACH ROW EXECUTE FUNCTION securities.reject_immutable_mutation();

INSERT INTO marketdata.instruments (
    id, symbol, display_name, asset_type, primary_exchange
) VALUES
    ('8f3c34b4-264e-4ccf-92fb-074b0a4b6981', 'AAPL', 'Apple Inc. (synthetic fixture)', 'COMMON_STOCK', 'NASDAQ'),
    ('26617db4-688d-49be-b89b-b1d890e6f0e2', 'MSFT', 'Microsoft Corp. (synthetic fixture)', 'COMMON_STOCK', 'NASDAQ'),
    ('1910fb85-ed90-42ce-9141-5d6409bc3c95', 'SPY', 'S&P 500 ETF (synthetic fixture)', 'ETF', 'NYSE ARCA'),
    ('044b33a5-c240-4f1e-875e-a37284b675bd', 'QQQ', 'Nasdaq 100 ETF (synthetic fixture)', 'ETF', 'NASDAQ'),
    ('57de05df-38b1-446d-89d2-d3b80ee39ea0', 'FIXADR', 'Synthetic ADR fixture', 'ADR', 'NASDAQ'),
    ('48e78ff8-547a-4b35-813c-9ae3494e7679', 'FIXREIT', 'Synthetic REIT fixture', 'REIT', 'NYSE'),
    ('1f228391-f57a-4386-a870-b3bcad6208d8', 'FIXPFD', 'Synthetic preferred fixture', 'PREFERRED', 'NYSE'),
    ('b2a2d83e-038d-447c-bfeb-3a0609a07146', 'FIXCEF', 'Synthetic closed-end fund fixture', 'CLOSED_END_FUND', 'NYSE'),
    ('c983255b-0918-477f-be00-80a0d38d44d1', 'FIXETN', 'Synthetic ETN fixture', 'ETN', 'NASDAQ'),
    ('0ed43c26-235b-4c86-b63a-43db9a6965c4', 'FIX.WS', 'Synthetic warrant fixture', 'WARRANT', 'NASDAQ'),
    ('e46f4df7-af85-4c73-8751-a2c8ef8e49b9', 'FIX.U', 'Synthetic unit fixture', 'UNIT', 'NYSE'),
    ('bc9eb128-10c7-411e-b662-80189318b66d', 'FIX.RT', 'Synthetic right fixture', 'RIGHT', 'NASDAQ');

INSERT INTO marketdata.instrument_capabilities (
    instrument_id,
    searchable,
    quote_enabled,
    paper_tradable,
    live_tradable,
    fractional_enabled,
    transfer_out_enabled,
    rwa_mint_enabled,
    user_eligible,
    disabled_reason,
    policy_version
)
SELECT
    instrument.id,
    TRUE,
    TRUE,
    instrument.asset_type IN ('COMMON_STOCK', 'ETF'),
    FALSE,
    instrument.asset_type IN ('COMMON_STOCK', 'ETF'),
    FALSE,
    FALSE,
    TRUE,
    CASE
        WHEN instrument.asset_type IN ('COMMON_STOCK', 'ETF') THEN NULL
        ELSE 'ASSET_TYPE_VIEW_ONLY'
    END,
    'paper-instrument-v1'
FROM marketdata.instruments AS instrument;

INSERT INTO marketdata.quote_fixtures (
    instrument_id, replay_cursor, bid, ask, last, status, market_status, observed_at
) VALUES
    ('8f3c34b4-264e-4ccf-92fb-074b0a4b6981', 0, 189.90, 190.10, 190.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('8f3c34b4-264e-4ccf-92fb-074b0a4b6981', 1, 189.95, 190.05, 190.00, 'SIMULATED', 'OPEN', '2026-07-19T14:31:00Z'),
    ('8f3c34b4-264e-4ccf-92fb-074b0a4b6981', 2, 190.05, 190.20, 190.10, 'SIMULATED', 'OPEN', '2026-07-19T14:32:00Z'),
    ('26617db4-688d-49be-b89b-b1d890e6f0e2', 0, 449.80, 450.20, 450.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('26617db4-688d-49be-b89b-b1d890e6f0e2', 1, 449.90, 450.10, 450.00, 'SIMULATED', 'OPEN', '2026-07-19T14:31:00Z'),
    ('1910fb85-ed90-42ce-9141-5d6409bc3c95', 0, 599.80, 600.20, 600.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('1910fb85-ed90-42ce-9141-5d6409bc3c95', 1, 599.90, 600.10, 600.00, 'SIMULATED', 'OPEN', '2026-07-19T14:31:00Z'),
    ('044b33a5-c240-4f1e-875e-a37284b675bd', 0, 499.80, 500.20, 500.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('044b33a5-c240-4f1e-875e-a37284b675bd', 1, 499.90, 500.10, 500.00, 'SIMULATED', 'OPEN', '2026-07-19T14:31:00Z'),
    ('57de05df-38b1-446d-89d2-d3b80ee39ea0', 0, 49.90, 50.10, 50.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('48e78ff8-547a-4b35-813c-9ae3494e7679', 0, 74.90, 75.10, 75.00, 'STALE', 'CLOSED', '2026-07-18T20:00:00Z'),
    ('1f228391-f57a-4386-a870-b3bcad6208d8', 0, 24.90, 25.10, 25.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('b2a2d83e-038d-447c-bfeb-3a0609a07146', 0, 9.90, 10.10, 10.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('c983255b-0918-477f-be00-80a0d38d44d1', 0, 14.90, 15.10, 15.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('0ed43c26-235b-4c86-b63a-43db9a6965c4', 0, 1.90, 2.10, 2.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('e46f4df7-af85-4c73-8751-a2c8ef8e49b9', 0, 19.90, 20.10, 20.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z'),
    ('bc9eb128-10c7-411e-b662-80189318b66d', 0, 0.90, 1.10, 1.00, 'SIMULATED', 'OPEN', '2026-07-19T14:30:00Z');

COMMENT ON TABLE marketdata.quote_fixtures IS
    'Deterministic local fixtures. Status is restricted so simulated prices can never be labeled live market data.';

COMMENT ON TABLE securities.broker_events IS
    'Immutable adapter events. Only the Securities application service may translate these events into Ledger commands.';

COMMENT ON TABLE securities.positions IS
    'Mutable read projection derived from immutable fills and Ledger-backed securities movements.';
