CREATE SCHEMA crypto;

CREATE FUNCTION crypto.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE crypto.assets (
    symbol VARCHAR(12) PRIMARY KEY,
    display_name TEXT NOT NULL,
    is_stablecoin BOOLEAN NOT NULL DEFAULT FALSE,
    tradable BOOLEAN NOT NULL DEFAULT TRUE,
    precision SMALLINT NOT NULL DEFAULT 18,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_assets_symbol_check CHECK (symbol ~ '^[A-Z][A-Z0-9]{2,11}$'),
    CONSTRAINT crypto_assets_name_check CHECK (display_name <> ''),
    CONSTRAINT crypto_assets_precision_check CHECK (precision BETWEEN 1 AND 18)
);

CREATE TABLE crypto.networks (
    network_code TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_networks_code_check CHECK (network_code ~ '^[A-Z][A-Z0-9_]{2,31}$'),
    CONSTRAINT crypto_networks_name_check CHECK (display_name <> '')
);

CREATE TABLE crypto.asset_networks (
    asset_symbol VARCHAR(12) NOT NULL REFERENCES crypto.assets(symbol) ON DELETE RESTRICT,
    network_code TEXT NOT NULL REFERENCES crypto.networks(network_code) ON DELETE RESTRICT,
    native_deployment BOOLEAN NOT NULL,
    deposit_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    withdrawal_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (asset_symbol, network_code),
    CONSTRAINT crypto_asset_networks_native_check CHECK (native_deployment)
);

CREATE TABLE crypto.venues (
    venue_code TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'SIMULATED',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_venues_code_check CHECK (venue_code IN ('VENUE_A', 'VENUE_B', 'VENUE_C')),
    CONSTRAINT crypto_venues_mode_check CHECK (mode = 'SIMULATED'),
    CONSTRAINT crypto_venues_name_check CHECK (display_name <> '')
);

CREATE TABLE crypto.policies (
    policy_version TEXT PRIMARY KEY,
    platform_fee_rate NUMERIC(38, 18) NOT NULL,
    conversion_review_single_usd NUMERIC(38, 18) NOT NULL,
    conversion_review_rolling_usd NUMERIC(38, 18) NOT NULL,
    conversion_review_window_days INTEGER NOT NULL,
    address_cooling_base_hours INTEGER NOT NULL,
    address_cooling_device_hours INTEGER NOT NULL,
    address_cooling_security_hours INTEGER NOT NULL,
    address_cooling_recovery_hours INTEGER NOT NULL,
    address_cooling_max_hours INTEGER NOT NULL,
    quote_max_age_seconds INTEGER NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_policies_version_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT crypto_policies_fee_check CHECK (platform_fee_rate >= 0 AND platform_fee_rate < 1),
    CONSTRAINT crypto_policies_review_check
        CHECK (conversion_review_single_usd > 0 AND conversion_review_rolling_usd > 0 AND conversion_review_window_days > 0),
    CONSTRAINT crypto_policies_cooling_check CHECK (
        address_cooling_base_hours > 0
        AND address_cooling_device_hours >= 0
        AND address_cooling_security_hours >= 0
        AND address_cooling_recovery_hours >= 0
        AND address_cooling_max_hours >= address_cooling_base_hours
    ),
    CONSTRAINT crypto_policies_quote_age_check CHECK (quote_max_age_seconds > 0)
);

CREATE UNIQUE INDEX crypto_policies_one_active_idx
ON crypto.policies (active)
WHERE active;

CREATE TABLE crypto.customer_profiles (
    customer_reference TEXT PRIMARY KEY,
    paper_account_id UUID NOT NULL UNIQUE REFERENCES securities.paper_accounts(id) ON DELETE RESTRICT,
    cash_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    policy_version TEXT NOT NULL REFERENCES crypto.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT crypto_profiles_customer_check CHECK (customer_reference <> ''),
    CONSTRAINT crypto_profiles_version_check CHECK (version > 0)
);

CREATE TABLE crypto.asset_ledger_accounts (
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    asset_symbol VARCHAR(12) NOT NULL REFERENCES crypto.assets(symbol) ON DELETE RESTRICT,
    customer_ledger_account_id UUID NOT NULL UNIQUE REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    custody_inventory_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, asset_symbol)
);

CREATE TABLE crypto.conversions (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    source_asset VARCHAR(12) NOT NULL,
    destination_asset VARCHAR(12) NOT NULL,
    source_amount NUMERIC(38, 18) NOT NULL,
    simulation_scenario TEXT NOT NULL DEFAULT 'NORMAL',
    status TEXT NOT NULL DEFAULT 'ROUTING',
    reason_code TEXT,
    next_action TEXT NOT NULL DEFAULT 'Routing across simulated USD venues.',
    policy_version TEXT NOT NULL REFERENCES crypto.policies(policy_version) ON DELETE RESTRICT,
    reservation_ledger_transaction_id UUID NOT NULL REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    compliance_case_id UUID REFERENCES compliance.cases(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT crypto_conversions_assets_check CHECK (
        source_asset <> destination_asset
        AND source_asset ~ '^[A-Z][A-Z0-9]{2,11}$'
        AND destination_asset ~ '^[A-Z][A-Z0-9]{2,11}$'
        AND NOT (source_asset = 'USD' AND destination_asset = 'USD')
    ),
    CONSTRAINT crypto_conversions_amount_check CHECK (source_amount > 0),
    CONSTRAINT crypto_conversions_scenario_check CHECK (
        simulation_scenario IN ('NORMAL', 'SPLIT', 'PARTIAL', 'TIMEOUT', 'STALE', 'VENUE_FAILURE', 'STABLECOIN_DEPEG')
    ),
    CONSTRAINT crypto_conversions_status_check CHECK (
        status IN ('ROUTING', 'FILLED', 'PARTIALLY_FILLED', 'REVIEW_REQUIRED', 'FAILED')
    ),
    CONSTRAINT crypto_conversions_reason_check
        CHECK (reason_code IS NULL OR reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT crypto_conversions_next_action_check CHECK (next_action <> ''),
    CONSTRAINT crypto_conversions_version_check CHECK (version > 0)
);

CREATE FUNCTION crypto.enforce_conversion_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.customer_reference <> NEW.customer_reference
        OR OLD.source_asset <> NEW.source_asset
        OR OLD.destination_asset <> NEW.destination_asset
        OR OLD.source_amount <> NEW.source_amount
        OR OLD.policy_version <> NEW.policy_version
        OR OLD.reservation_ledger_transaction_id <> NEW.reservation_ledger_transaction_id THEN
        RAISE EXCEPTION 'conversion request identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'ROUTING' OR NEW.status NOT IN ('FILLED', 'PARTIALLY_FILLED', 'REVIEW_REQUIRED', 'FAILED') THEN
        RAISE EXCEPTION 'invalid conversion transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER crypto_conversions_state_transition
BEFORE UPDATE ON crypto.conversions
FOR EACH ROW EXECUTE FUNCTION crypto.enforce_conversion_transition();

CREATE TABLE crypto.conversion_requests (
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    conversion_id UUID NOT NULL UNIQUE REFERENCES crypto.conversions(id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, idempotency_key),
    CONSTRAINT crypto_conversion_requests_key_check
        CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT crypto_conversion_requests_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE crypto.legs (
    id UUID PRIMARY KEY,
    conversion_id UUID NOT NULL REFERENCES crypto.conversions(id) ON DELETE RESTRICT,
    leg_sequence SMALLINT NOT NULL,
    side TEXT NOT NULL,
    asset_symbol VARCHAR(12) NOT NULL REFERENCES crypto.assets(symbol) ON DELETE RESTRICT,
    quote_currency VARCHAR(12) NOT NULL DEFAULT 'USD',
    input_amount NUMERIC(38, 18) NOT NULL,
    filled_quantity NUMERIC(38, 18) NOT NULL DEFAULT 0,
    reference_price NUMERIC(38, 18) NOT NULL,
    average_execution_price NUMERIC(38, 18) NOT NULL DEFAULT 0,
    gross_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    venue_fee_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    platform_fee_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    final_customer_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    price_improvement_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    failure_code TEXT,
    ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_legs_sequence_check CHECK (leg_sequence IN (1, 2)),
    CONSTRAINT crypto_legs_side_check CHECK (side IN ('BUY', 'SELL')),
    CONSTRAINT crypto_legs_usd_only_check CHECK (quote_currency = 'USD' AND asset_symbol <> 'USD'),
    CONSTRAINT crypto_legs_amount_check CHECK (
        input_amount > 0 AND filled_quantity >= 0 AND reference_price > 0
        AND average_execution_price >= 0 AND gross_usd >= 0 AND venue_fee_usd >= 0
        AND platform_fee_usd >= 0 AND final_customer_usd >= 0 AND price_improvement_usd >= 0
    ),
    CONSTRAINT crypto_legs_status_check CHECK (status IN ('FILLED', 'PARTIALLY_FILLED', 'FAILED', 'BLOCKED')),
    CONSTRAINT crypto_legs_failure_check
        CHECK (failure_code IS NULL OR failure_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    UNIQUE (conversion_id, leg_sequence)
);

CREATE TABLE crypto.child_orders (
    id UUID PRIMARY KEY,
    leg_id UUID NOT NULL REFERENCES crypto.legs(id) ON DELETE RESTRICT,
    child_sequence SMALLINT NOT NULL,
    venue_code TEXT NOT NULL REFERENCES crypto.venues(venue_code) ON DELETE RESTRICT,
    client_order_id TEXT NOT NULL,
    provider_order_id TEXT,
    requested_quantity NUMERIC(38, 18) NOT NULL,
    filled_quantity NUMERIC(38, 18) NOT NULL DEFAULT 0,
    quote_bid NUMERIC(38, 18) NOT NULL,
    quote_ask NUMERIC(38, 18) NOT NULL,
    venue_fee_rate NUMERIC(38, 18) NOT NULL,
    effective_unit_price NUMERIC(38, 18) NOT NULL,
    execution_price NUMERIC(38, 18) NOT NULL DEFAULT 0,
    gross_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    venue_fee_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    failure_code TEXT,
    quote_observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_children_sequence_check CHECK (child_sequence > 0),
    CONSTRAINT crypto_children_client_check CHECK (client_order_id <> ''),
    CONSTRAINT crypto_children_amount_check CHECK (
        requested_quantity > 0 AND filled_quantity >= 0 AND filled_quantity <= requested_quantity
        AND quote_bid > 0 AND quote_ask > 0 AND quote_bid <= quote_ask
        AND venue_fee_rate >= 0 AND venue_fee_rate < 1 AND effective_unit_price > 0
        AND execution_price >= 0 AND gross_usd >= 0 AND venue_fee_usd >= 0
    ),
    CONSTRAINT crypto_children_status_check CHECK (
        status IN ('FILLED', 'PARTIALLY_FILLED', 'FAILED', 'TIMEOUT', 'STALE', 'SKIPPED')
    ),
    CONSTRAINT crypto_children_failure_check
        CHECK (failure_code IS NULL OR failure_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    UNIQUE (leg_id, child_sequence),
    UNIQUE (venue_code, client_order_id)
);

CREATE TABLE crypto.fills (
    id UUID PRIMARY KEY,
    child_order_id UUID NOT NULL REFERENCES crypto.child_orders(id) ON DELETE RESTRICT,
    venue_code TEXT NOT NULL REFERENCES crypto.venues(venue_code) ON DELETE RESTRICT,
    external_fill_id TEXT NOT NULL,
    quantity NUMERIC(38, 18) NOT NULL,
    price NUMERIC(38, 18) NOT NULL,
    gross_usd NUMERIC(38, 18) NOT NULL,
    venue_fee_usd NUMERIC(38, 18) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_fills_external_check CHECK (external_fill_id <> ''),
    CONSTRAINT crypto_fills_amount_check CHECK (quantity > 0 AND price > 0 AND gross_usd > 0 AND venue_fee_usd >= 0),
    UNIQUE (venue_code, external_fill_id)
);

CREATE TABLE crypto.deposit_addresses (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    asset_symbol VARCHAR(12) NOT NULL REFERENCES crypto.assets(symbol) ON DELETE RESTRICT,
    network_code TEXT NOT NULL,
    provider TEXT NOT NULL,
    external_address TEXT NOT NULL,
    memo TEXT,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_deposit_addresses_network_fk
        FOREIGN KEY (asset_symbol, network_code) REFERENCES crypto.asset_networks(asset_symbol, network_code) ON DELETE RESTRICT,
    CONSTRAINT crypto_deposit_addresses_provider_check
        CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT crypto_deposit_addresses_external_check CHECK (length(external_address) BETWEEN 8 AND 255),
    CONSTRAINT crypto_deposit_addresses_status_check CHECK (status IN ('ACTIVE', 'SUSPENDED')),
    UNIQUE (customer_reference, asset_symbol, network_code),
    UNIQUE (provider, external_address)
);

CREATE TABLE crypto.deposit_address_requests (
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    deposit_address_id UUID NOT NULL UNIQUE REFERENCES crypto.deposit_addresses(id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, idempotency_key),
    CONSTRAINT crypto_deposit_address_requests_key_check
        CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT crypto_deposit_address_requests_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE crypto.deposits (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    deposit_address_id UUID NOT NULL REFERENCES crypto.deposit_addresses(id) ON DELETE RESTRICT,
    asset_symbol VARCHAR(12) NOT NULL REFERENCES crypto.assets(symbol) ON DELETE RESTRICT,
    network_code TEXT NOT NULL REFERENCES crypto.networks(network_code) ON DELETE RESTRICT,
    quantity NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL,
    provider TEXT NOT NULL,
    provider_transaction_id TEXT NOT NULL,
    ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    reason_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT crypto_deposits_quantity_check CHECK (quantity > 0),
    CONSTRAINT crypto_deposits_status_check CHECK (status IN ('CONFIRMED', 'REJECTED')),
    CONSTRAINT crypto_deposits_provider_check CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT crypto_deposits_provider_id_check CHECK (provider_transaction_id <> ''),
    CONSTRAINT crypto_deposits_reason_check
        CHECK (reason_code IS NULL OR reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT crypto_deposits_version_check CHECK (version > 0),
    UNIQUE (provider, provider_transaction_id)
);

CREATE TABLE crypto.withdrawal_addresses (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    asset_symbol VARCHAR(12) NOT NULL REFERENCES crypto.assets(symbol) ON DELETE RESTRICT,
    network_code TEXT NOT NULL,
    external_address TEXT NOT NULL,
    label TEXT NOT NULL,
    status TEXT NOT NULL,
    risk_level TEXT NOT NULL,
    cooling_until TIMESTAMPTZ,
    cooling_reason TEXT NOT NULL,
    policy_version TEXT NOT NULL REFERENCES crypto.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT crypto_withdrawal_addresses_network_fk
        FOREIGN KEY (asset_symbol, network_code) REFERENCES crypto.asset_networks(asset_symbol, network_code) ON DELETE RESTRICT,
    CONSTRAINT crypto_withdrawal_addresses_external_check CHECK (length(external_address) BETWEEN 8 AND 255),
    CONSTRAINT crypto_withdrawal_addresses_label_check CHECK (label <> ''),
    CONSTRAINT crypto_withdrawal_addresses_status_check CHECK (
        status IN ('DRAFT', 'VERIFICATION_PENDING', 'COOLING_OFF', 'ACTIVE', 'SUSPENDED', 'REVOKED')
    ),
    CONSTRAINT crypto_withdrawal_addresses_risk_check CHECK (risk_level IN ('LOW', 'ELEVATED', 'BLOCKED')),
    CONSTRAINT crypto_withdrawal_addresses_cooling_check
        CHECK ((status <> 'COOLING_OFF') OR cooling_until IS NOT NULL),
    CONSTRAINT crypto_withdrawal_addresses_reason_check CHECK (cooling_reason <> ''),
    CONSTRAINT crypto_withdrawal_addresses_version_check CHECK (version > 0),
    UNIQUE (customer_reference, asset_symbol, network_code, external_address)
);

CREATE FUNCTION crypto.enforce_withdrawal_address_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.customer_reference <> NEW.customer_reference
        OR OLD.asset_symbol <> NEW.asset_symbol
        OR OLD.network_code <> NEW.network_code
        OR OLD.external_address <> NEW.external_address THEN
        RAISE EXCEPTION 'withdrawal address identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> NEW.status AND NOT (
        (OLD.status = 'VERIFICATION_PENDING' AND NEW.status IN ('COOLING_OFF', 'SUSPENDED', 'REVOKED'))
        OR (OLD.status = 'COOLING_OFF' AND NEW.status IN ('ACTIVE', 'SUSPENDED', 'REVOKED'))
        OR (OLD.status = 'ACTIVE' AND NEW.status IN ('SUSPENDED', 'REVOKED'))
        OR (OLD.status = 'SUSPENDED' AND NEW.status IN ('COOLING_OFF', 'REVOKED'))
    ) THEN
        RAISE EXCEPTION 'invalid withdrawal address transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER crypto_withdrawal_addresses_state_transition
BEFORE UPDATE ON crypto.withdrawal_addresses
FOR EACH ROW EXECUTE FUNCTION crypto.enforce_withdrawal_address_transition();

CREATE TABLE crypto.withdrawal_address_requests (
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    address_id UUID NOT NULL UNIQUE REFERENCES crypto.withdrawal_addresses(id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, idempotency_key),
    CONSTRAINT crypto_withdrawal_address_requests_key_check
        CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT crypto_withdrawal_address_requests_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE crypto.withdrawal_address_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    address_id UUID NOT NULL REFERENCES crypto.withdrawal_addresses(id) ON DELETE RESTRICT,
    from_status TEXT,
    to_status TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_withdrawal_address_events_actor_check
        CHECK (actor_type IN ('USER', 'ADMIN', 'SYSTEM', 'PROVIDER') AND actor_id <> ''),
    CONSTRAINT crypto_withdrawal_address_events_reason_check
        CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT crypto_withdrawal_address_events_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE crypto.withdrawals (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    withdrawal_address_id UUID NOT NULL REFERENCES crypto.withdrawal_addresses(id) ON DELETE RESTRICT,
    asset_symbol VARCHAR(12) NOT NULL REFERENCES crypto.assets(symbol) ON DELETE RESTRICT,
    network_code TEXT NOT NULL REFERENCES crypto.networks(network_code) ON DELETE RESTRICT,
    quantity NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    next_action TEXT NOT NULL,
    provider TEXT NOT NULL,
    provider_withdrawal_id TEXT NOT NULL,
    reservation_ledger_transaction_id UUID NOT NULL REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    broadcast_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    reservation_reversal_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    broadcast_reversal_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    policy_version TEXT NOT NULL REFERENCES crypto.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT crypto_withdrawals_quantity_check CHECK (quantity > 0),
    CONSTRAINT crypto_withdrawals_status_check CHECK (status IN ('APPROVED', 'BROADCAST', 'CONFIRMED', 'FAILED', 'CANCELED')),
    CONSTRAINT crypto_withdrawals_reason_check CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT crypto_withdrawals_next_action_check CHECK (next_action <> ''),
    CONSTRAINT crypto_withdrawals_provider_check CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT crypto_withdrawals_provider_id_check CHECK (provider_withdrawal_id <> ''),
    CONSTRAINT crypto_withdrawals_version_check CHECK (version > 0),
    UNIQUE (provider, provider_withdrawal_id)
);

CREATE FUNCTION crypto.enforce_withdrawal_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.customer_reference <> NEW.customer_reference
        OR OLD.withdrawal_address_id <> NEW.withdrawal_address_id
        OR OLD.asset_symbol <> NEW.asset_symbol
        OR OLD.network_code <> NEW.network_code
        OR OLD.quantity <> NEW.quantity
        OR OLD.reservation_ledger_transaction_id <> NEW.reservation_ledger_transaction_id THEN
        RAISE EXCEPTION 'crypto withdrawal identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> NEW.status AND NOT (
        (OLD.status = 'APPROVED' AND NEW.status IN ('BROADCAST', 'FAILED', 'CANCELED'))
        OR (OLD.status = 'BROADCAST' AND NEW.status IN ('CONFIRMED', 'FAILED'))
    ) THEN
        RAISE EXCEPTION 'invalid crypto withdrawal transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER crypto_withdrawals_state_transition
BEFORE UPDATE ON crypto.withdrawals
FOR EACH ROW EXECUTE FUNCTION crypto.enforce_withdrawal_transition();

CREATE TABLE crypto.withdrawal_requests (
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    withdrawal_id UUID NOT NULL UNIQUE REFERENCES crypto.withdrawals(id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, idempotency_key),
    CONSTRAINT crypto_withdrawal_requests_key_check
        CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT crypto_withdrawal_requests_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE crypto.provider_events (
    provider TEXT NOT NULL,
    external_event_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    domain_record_id UUID NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (provider, external_event_id),
    CONSTRAINT crypto_provider_events_provider_check CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT crypto_provider_events_external_check
        CHECK (external_event_id <> '' AND length(external_event_id) <= 255),
    CONSTRAINT crypto_provider_events_resource_check CHECK (resource_type IN ('DEPOSIT', 'WITHDRAWAL')),
    CONSTRAINT crypto_provider_events_type_check CHECK (event_type ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT crypto_provider_events_hash_check CHECK (payload_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE crypto.reconciliation_runs (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES crypto.customer_profiles(customer_reference) ON DELETE RESTRICT,
    asset_symbol VARCHAR(12) NOT NULL REFERENCES crypto.assets(symbol) ON DELETE RESTRICT,
    ledger_amount NUMERIC(38, 18) NOT NULL,
    custody_amount NUMERIC(38, 18) NOT NULL,
    difference NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL,
    compliance_case_id UUID REFERENCES compliance.cases(id) ON DELETE RESTRICT,
    policy_version TEXT NOT NULL REFERENCES crypto.policies(policy_version) ON DELETE RESTRICT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT crypto_reconciliation_status_check CHECK (status IN ('MATCHED', 'DIFFERENCE')),
    CONSTRAINT crypto_reconciliation_difference_check
        CHECK ((status = 'MATCHED' AND difference = 0) OR (status = 'DIFFERENCE' AND difference <> 0))
);

CREATE TRIGGER crypto_conversion_requests_immutable
BEFORE UPDATE OR DELETE ON crypto.conversion_requests
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_legs_immutable
BEFORE UPDATE OR DELETE ON crypto.legs
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_child_orders_immutable
BEFORE UPDATE OR DELETE ON crypto.child_orders
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_fills_immutable
BEFORE UPDATE OR DELETE ON crypto.fills
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_deposit_address_requests_immutable
BEFORE UPDATE OR DELETE ON crypto.deposit_address_requests
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_deposits_immutable
BEFORE UPDATE OR DELETE ON crypto.deposits
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_withdrawal_address_requests_immutable
BEFORE UPDATE OR DELETE ON crypto.withdrawal_address_requests
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_withdrawal_address_events_immutable
BEFORE UPDATE OR DELETE ON crypto.withdrawal_address_events
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_withdrawal_requests_immutable
BEFORE UPDATE OR DELETE ON crypto.withdrawal_requests
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_provider_events_immutable
BEFORE UPDATE OR DELETE ON crypto.provider_events
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE TRIGGER crypto_reconciliation_runs_immutable
BEFORE UPDATE OR DELETE ON crypto.reconciliation_runs
FOR EACH ROW EXECUTE FUNCTION crypto.reject_immutable_mutation();

CREATE INDEX crypto_conversions_customer_created_idx
ON crypto.conversions (customer_reference, created_at DESC, id DESC);

CREATE INDEX crypto_withdrawal_addresses_customer_idx
ON crypto.withdrawal_addresses (customer_reference, asset_symbol, created_at DESC);

CREATE INDEX crypto_withdrawals_customer_created_idx
ON crypto.withdrawals (customer_reference, created_at DESC, id DESC);

CREATE INDEX crypto_deposits_customer_created_idx
ON crypto.deposits (customer_reference, created_at DESC, id DESC);

INSERT INTO crypto.assets (symbol, display_name, is_stablecoin) VALUES
    ('BTC', 'Bitcoin', FALSE),
    ('ETH', 'Ethereum', FALSE),
    ('SOL', 'Solana', FALSE),
    ('XRP', 'XRP', FALSE),
    ('BNB', 'BNB', FALSE),
    ('DOGE', 'Dogecoin', FALSE),
    ('ADA', 'Cardano', FALSE),
    ('AVAX', 'Avalanche', FALSE),
    ('LINK', 'Chainlink', FALSE),
    ('LTC', 'Litecoin', FALSE),
    ('USDC', 'USD Coin', TRUE),
    ('USDT', 'Tether USD', TRUE);

INSERT INTO crypto.networks (network_code, display_name) VALUES
    ('BITCOIN', 'Bitcoin'),
    ('ETHEREUM', 'Ethereum'),
    ('BASE', 'Base'),
    ('SOLANA', 'Solana'),
    ('ARBITRUM', 'Arbitrum'),
    ('POLYGON', 'Polygon'),
    ('TRON', 'Tron'),
    ('TON', 'TON'),
    ('AVALANCHE_C', 'Avalanche C-Chain'),
    ('XRP_LEDGER', 'XRP Ledger'),
    ('BNB_SMART_CHAIN', 'BNB Smart Chain'),
    ('DOGECOIN', 'Dogecoin'),
    ('CARDANO', 'Cardano'),
    ('LITECOIN', 'Litecoin');

INSERT INTO crypto.asset_networks (asset_symbol, network_code, native_deployment) VALUES
    ('BTC', 'BITCOIN', TRUE),
    ('ETH', 'ETHEREUM', TRUE),
    ('SOL', 'SOLANA', TRUE),
    ('XRP', 'XRP_LEDGER', TRUE),
    ('BNB', 'BNB_SMART_CHAIN', TRUE),
    ('DOGE', 'DOGECOIN', TRUE),
    ('ADA', 'CARDANO', TRUE),
    ('AVAX', 'AVALANCHE_C', TRUE),
    ('LINK', 'ETHEREUM', TRUE),
    ('LTC', 'LITECOIN', TRUE),
    ('USDC', 'ETHEREUM', TRUE),
    ('USDC', 'BASE', TRUE),
    ('USDC', 'SOLANA', TRUE),
    ('USDC', 'ARBITRUM', TRUE),
    ('USDC', 'POLYGON', TRUE),
    ('USDT', 'ETHEREUM', TRUE),
    ('USDT', 'TRON', TRUE),
    ('USDT', 'SOLANA', TRUE),
    ('USDT', 'TON', TRUE),
    ('USDT', 'AVALANCHE_C', TRUE);

INSERT INTO crypto.venues (venue_code, display_name) VALUES
    ('VENUE_A', 'Local Liquidity Venue A'),
    ('VENUE_B', 'Local Liquidity Venue B'),
    ('VENUE_C', 'Local Liquidity Venue C');

INSERT INTO crypto.policies (
    policy_version,
    platform_fee_rate,
    conversion_review_single_usd,
    conversion_review_rolling_usd,
    conversion_review_window_days,
    address_cooling_base_hours,
    address_cooling_device_hours,
    address_cooling_security_hours,
    address_cooling_recovery_hours,
    address_cooling_max_hours,
    quote_max_age_seconds,
    effective_at,
    active
) VALUES (
    'crypto-sim-v1',
    0.002,
    25000,
    25000,
    30,
    24,
    24,
    24,
    48,
    72,
    5,
    '2026-01-01T00:00:00Z',
    TRUE
);

COMMENT ON TABLE crypto.legs IS
    'Every leg is quoted and settled only in USD. Cross-asset conversions require two explicit rows.';

COMMENT ON TABLE crypto.provider_events IS
    'Immutable custody callback inbox. Providers cannot write Ledger balances directly.';

COMMENT ON COLUMN crypto.policies.platform_fee_rate IS
    'Explicit, versioned simulator fee. Production fees require approved configuration.';
