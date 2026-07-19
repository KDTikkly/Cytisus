CREATE SCHEMA rwa;

CREATE FUNCTION rwa.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

CREATE TABLE securities.position_reservations (
    id UUID PRIMARY KEY,
    paper_account_id UUID NOT NULL REFERENCES securities.paper_accounts(id) ON DELETE RESTRICT,
    instrument_id UUID NOT NULL REFERENCES marketdata.instruments(id) ON DELETE RESTRICT,
    symbol TEXT NOT NULL,
    reservation_type TEXT NOT NULL,
    quantity NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    customer_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    locked_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    lock_ledger_transaction_id UUID NOT NULL REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    release_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    released_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT securities_position_reservations_symbol_check CHECK (symbol ~ '^[A-Z][A-Z0-9.-]{0,15}$'),
    CONSTRAINT securities_position_reservations_type_check CHECK (reservation_type = 'RWA_LOCK'),
    CONSTRAINT securities_position_reservations_quantity_check CHECK (quantity > 0 AND quantity = trunc(quantity)),
    CONSTRAINT securities_position_reservations_status_check CHECK (status IN ('ACTIVE', 'RELEASED')),
    CONSTRAINT securities_position_reservations_release_check CHECK (
        (status = 'ACTIVE' AND released_at IS NULL AND release_ledger_transaction_id IS NULL)
        OR (status = 'RELEASED' AND released_at IS NOT NULL AND release_ledger_transaction_id IS NOT NULL)
    ),
    CONSTRAINT securities_position_reservations_version_check CHECK (version > 0)
);

CREATE INDEX securities_position_reservations_active_idx
ON securities.position_reservations (paper_account_id, instrument_id)
WHERE status = 'ACTIVE';

CREATE TABLE rwa.policies (
    policy_version TEXT PRIMARY KEY,
    chain_name TEXT NOT NULL,
    chain_id BIGINT NOT NULL UNIQUE,
    custody_mode TEXT NOT NULL,
    address_cooling_hours INTEGER NOT NULL,
    recovery_max_attempts INTEGER NOT NULL,
    active BOOLEAN NOT NULL DEFAULT FALSE,
    effective_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT rwa_policies_version_check CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT rwa_policies_chain_check CHECK (chain_name = 'BASE_ANVIL' AND chain_id = 84532),
    CONSTRAINT rwa_policies_custody_check CHECK (custody_mode = 'PLATFORM_VAULT'),
    CONSTRAINT rwa_policies_recovery_check CHECK (address_cooling_hours > 0 AND recovery_max_attempts BETWEEN 1 AND 100)
);

CREATE UNIQUE INDEX rwa_policies_one_active_idx ON rwa.policies (active) WHERE active;

CREATE TABLE rwa.assets (
    id UUID PRIMARY KEY,
    instrument_id UUID NOT NULL UNIQUE REFERENCES marketdata.instruments(id) ON DELETE RESTRICT,
    underlying_symbol TEXT NOT NULL UNIQUE,
    token_name TEXT NOT NULL,
    token_symbol TEXT NOT NULL UNIQUE,
    token_decimals SMALLINT NOT NULL DEFAULT 0,
    contract_address TEXT NOT NULL UNIQUE,
    platform_vault_address TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    simulation_disclaimer TEXT NOT NULL,
    policy_version TEXT NOT NULL REFERENCES rwa.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT rwa_assets_symbol_check CHECK (underlying_symbol ~ '^[A-Z][A-Z0-9.-]{0,15}$'),
    CONSTRAINT rwa_assets_token_check CHECK (token_name <> '' AND token_symbol ~ '^[A-Z][A-Z0-9]{1,11}$'),
    CONSTRAINT rwa_assets_decimals_check CHECK (token_decimals = 0),
    CONSTRAINT rwa_assets_contract_check CHECK (contract_address ~ '^0x[0-9a-fA-F]{40}$'),
    CONSTRAINT rwa_assets_vault_check CHECK (platform_vault_address ~ '^0x[0-9a-fA-F]{40}$'),
    CONSTRAINT rwa_assets_disclaimer_check CHECK (simulation_disclaimer = 'SIMULATION_ONLY_NOT_A_LEGALLY_ISSUED_SECURITY')
);

CREATE TABLE rwa.customer_profiles (
    customer_reference TEXT PRIMARY KEY,
    paper_account_id UUID NOT NULL UNIQUE REFERENCES securities.paper_accounts(id) ON DELETE RESTRICT,
    cash_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'ELIGIBLE',
    policy_version TEXT NOT NULL REFERENCES rwa.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT rwa_profiles_customer_check CHECK (customer_reference <> ''),
    CONSTRAINT rwa_profiles_status_check CHECK (status IN ('ELIGIBLE', 'REVIEW_REQUIRED', 'INELIGIBLE')),
    CONSTRAINT rwa_profiles_version_check CHECK (version > 0)
);

CREATE TABLE rwa.external_addresses (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES rwa.customer_profiles(customer_reference) ON DELETE RESTRICT,
    address TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING_PROOF',
    proof_reference TEXT,
    proof_verified_at TIMESTAMPTZ,
    risk_reason_code TEXT,
    risk_reviewed_at TIMESTAMPTZ,
    cooling_ends_at TIMESTAMPTZ,
    activated_at TIMESTAMPTZ,
    suspended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT rwa_addresses_format_check CHECK (address ~ '^0x[0-9a-fA-F]{40}$'),
    CONSTRAINT rwa_addresses_status_check CHECK (status IN (
        'PENDING_PROOF', 'PROOF_VERIFIED', 'RISK_REVIEW', 'COOLING', 'ACTIVE', 'REJECTED', 'SUSPENDED'
    )),
    CONSTRAINT rwa_addresses_proof_check CHECK (
        status = 'PENDING_PROOF'
        OR (proof_reference <> '' AND proof_verified_at IS NOT NULL)
    ),
    CONSTRAINT rwa_addresses_cooling_check CHECK (
        status NOT IN ('COOLING', 'ACTIVE') OR cooling_ends_at IS NOT NULL
    ),
    CONSTRAINT rwa_addresses_active_check CHECK (
        (status = 'ACTIVE' AND activated_at IS NOT NULL AND activated_at >= cooling_ends_at)
        OR (status <> 'ACTIVE' AND activated_at IS NULL)
    ),
    CONSTRAINT rwa_addresses_suspended_check CHECK ((status = 'SUSPENDED') = (suspended_at IS NOT NULL)),
    CONSTRAINT rwa_addresses_version_check CHECK (version > 0),
    UNIQUE (customer_reference, address)
);

CREATE UNIQUE INDEX rwa_one_active_external_address_idx
ON rwa.external_addresses (customer_reference)
WHERE status = 'ACTIVE';

CREATE TABLE rwa.command_requests (
    customer_reference TEXT NOT NULL REFERENCES rwa.customer_profiles(customer_reference) ON DELETE RESTRICT,
    scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    resource_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, scope, idempotency_key),
    CONSTRAINT rwa_command_scope_check CHECK (scope ~ '^[a-z][a-z0-9._:-]{2,127}$'),
    CONSTRAINT rwa_command_key_check CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT rwa_command_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE rwa.underlying_locks (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES rwa.customer_profiles(customer_reference) ON DELETE RESTRICT,
    asset_id UUID NOT NULL REFERENCES rwa.assets(id) ON DELETE RESTRICT,
    reservation_id UUID NOT NULL UNIQUE REFERENCES securities.position_reservations(id) ON DELETE RESTRICT,
    quantity NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL DEFAULT 'LOCKED',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    released_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT rwa_locks_quantity_check CHECK (quantity > 0 AND quantity = trunc(quantity)),
    CONSTRAINT rwa_locks_status_check CHECK (status IN ('LOCKED', 'RELEASED')),
    CONSTRAINT rwa_locks_release_check CHECK ((status = 'RELEASED') = (released_at IS NOT NULL)),
    CONSTRAINT rwa_locks_version_check CHECK (version > 0)
);

CREATE TABLE rwa.mint_requests (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES rwa.customer_profiles(customer_reference) ON DELETE RESTRICT,
    asset_id UUID NOT NULL REFERENCES rwa.assets(id) ON DELETE RESTRICT,
    underlying_lock_id UUID NOT NULL UNIQUE REFERENCES rwa.underlying_locks(id) ON DELETE RESTRICT,
    quantity NUMERIC(38, 18) NOT NULL,
    custody_mode TEXT NOT NULL,
    destination_address_id UUID REFERENCES rwa.external_addresses(id) ON DELETE RESTRICT,
    destination_address TEXT NOT NULL,
    operation_id CHAR(64) NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'UNDERLYING_LOCKED',
    chain_tx_hash TEXT,
    failure_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT rwa_mints_quantity_check CHECK (quantity > 0 AND quantity = trunc(quantity)),
    CONSTRAINT rwa_mints_custody_check CHECK (custody_mode IN ('PLATFORM_VAULT', 'EXTERNAL_PERMISSIONED')),
    CONSTRAINT rwa_mints_destination_check CHECK (
        destination_address ~ '^0x[0-9a-fA-F]{40}$'
        AND ((custody_mode = 'PLATFORM_VAULT' AND destination_address_id IS NULL)
          OR (custody_mode = 'EXTERNAL_PERMISSIONED' AND destination_address_id IS NOT NULL))
    ),
    CONSTRAINT rwa_mints_operation_check CHECK (operation_id ~ '^[0-9a-f]{64}$'),
    CONSTRAINT rwa_mints_status_check CHECK (status IN (
        'UNDERLYING_LOCKED', 'CHAIN_PENDING', 'MINTED', 'RECOVERY_REQUIRED', 'FAILED_RECOVERED'
    )),
    CONSTRAINT rwa_mints_tx_check CHECK (chain_tx_hash IS NULL OR chain_tx_hash ~ '^0x[0-9a-fA-F]{64}$'),
    CONSTRAINT rwa_mints_failure_check CHECK (
        (status IN ('RECOVERY_REQUIRED', 'FAILED_RECOVERED') AND failure_code ~ '^[A-Z][A-Z0-9_]{2,63}$')
        OR (status NOT IN ('RECOVERY_REQUIRED', 'FAILED_RECOVERED') AND failure_code IS NULL)
    ),
    CONSTRAINT rwa_mints_version_check CHECK (version > 0)
);

CREATE TABLE rwa.beneficial_holdings (
    customer_reference TEXT NOT NULL REFERENCES rwa.customer_profiles(customer_reference) ON DELETE RESTRICT,
    asset_id UUID NOT NULL REFERENCES rwa.assets(id) ON DELETE RESTRICT,
    custody_mode TEXT NOT NULL,
    destination_address TEXT NOT NULL,
    quantity NUMERIC(38, 18) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (customer_reference, asset_id),
    CONSTRAINT rwa_holdings_custody_check CHECK (custody_mode IN ('PLATFORM_VAULT', 'EXTERNAL_PERMISSIONED')),
    CONSTRAINT rwa_holdings_address_check CHECK (destination_address ~ '^0x[0-9a-fA-F]{40}$'),
    CONSTRAINT rwa_holdings_quantity_check CHECK (quantity >= 0 AND quantity = trunc(quantity)),
    CONSTRAINT rwa_holdings_version_check CHECK (version > 0)
);

CREATE TABLE rwa.redemption_requests (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES rwa.customer_profiles(customer_reference) ON DELETE RESTRICT,
    asset_id UUID NOT NULL REFERENCES rwa.assets(id) ON DELETE RESTRICT,
    underlying_lock_id UUID NOT NULL UNIQUE REFERENCES rwa.underlying_locks(id) ON DELETE RESTRICT,
    quantity NUMERIC(38, 18) NOT NULL,
    source_address TEXT NOT NULL,
    operation_id CHAR(64) NOT NULL UNIQUE,
    forced BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'REQUESTED',
    chain_tx_hash TEXT,
    failure_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT rwa_redemptions_quantity_check CHECK (quantity > 0 AND quantity = trunc(quantity)),
    CONSTRAINT rwa_redemptions_address_check CHECK (source_address ~ '^0x[0-9a-fA-F]{40}$'),
    CONSTRAINT rwa_redemptions_operation_check CHECK (operation_id ~ '^[0-9a-f]{64}$'),
    CONSTRAINT rwa_redemptions_status_check CHECK (status IN (
        'REQUESTED', 'CHAIN_PENDING', 'BURN_CONFIRMED', 'RECOVERY_REQUIRED', 'COMPLETED'
    )),
    CONSTRAINT rwa_redemptions_tx_check CHECK (chain_tx_hash IS NULL OR chain_tx_hash ~ '^0x[0-9a-fA-F]{64}$'),
    CONSTRAINT rwa_redemptions_failure_check CHECK (
        (status = 'RECOVERY_REQUIRED' AND failure_code ~ '^[A-Z][A-Z0-9_]{2,63}$')
        OR (status <> 'RECOVERY_REQUIRED' AND failure_code IS NULL)
    ),
    CONSTRAINT rwa_redemptions_complete_check CHECK ((status = 'COMPLETED') = (completed_at IS NOT NULL)),
    CONSTRAINT rwa_redemptions_version_check CHECK (version > 0)
);

CREATE TABLE rwa.chain_operations (
    operation_id CHAR(64) PRIMARY KEY,
    resource_type TEXT NOT NULL,
    resource_id UUID NOT NULL,
    operation_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    tx_hash TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    last_error_code TEXT,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT rwa_operations_id_check CHECK (operation_id ~ '^[0-9a-f]{64}$'),
    CONSTRAINT rwa_operations_resource_check CHECK (resource_type IN ('MINT', 'REDEMPTION')),
    CONSTRAINT rwa_operations_type_check CHECK (operation_type IN ('MINT', 'BURN', 'FORCED_REDEMPTION')),
    CONSTRAINT rwa_operations_status_check CHECK (status IN ('PENDING', 'SUBMITTED', 'CONFIRMED', 'UNKNOWN', 'FAILED', 'DLQ')),
    CONSTRAINT rwa_operations_tx_check CHECK (tx_hash IS NULL OR tx_hash ~ '^0x[0-9a-fA-F]{64}$'),
    CONSTRAINT rwa_operations_attempt_check CHECK (attempt_count >= 0 AND max_attempts > 0),
    CONSTRAINT rwa_operations_claim_check CHECK ((claimed_at IS NULL) = (claimed_by IS NULL))
);

CREATE INDEX rwa_chain_operations_claim_idx
ON rwa.chain_operations (next_attempt_at, created_at)
WHERE status IN ('PENDING', 'UNKNOWN');

CREATE TABLE rwa.provider_events (
    provider TEXT NOT NULL,
    external_event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    operation_id CHAR(64) NOT NULL REFERENCES rwa.chain_operations(operation_id) ON DELETE RESTRICT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (provider, external_event_id),
    CONSTRAINT rwa_provider_name_check CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT rwa_provider_external_check CHECK (external_event_id <> ''),
    CONSTRAINT rwa_provider_type_check CHECK (event_type ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT rwa_provider_hash_check CHECK (payload_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE rwa.state_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_type TEXT NOT NULL,
    resource_id UUID NOT NULL,
    from_status TEXT,
    to_status TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    reason_code TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT rwa_events_resource_check CHECK (resource_type IN ('ADDRESS', 'MINT', 'REDEMPTION', 'DIVIDEND')),
    CONSTRAINT rwa_events_actor_check CHECK (actor_type IN ('USER', 'ADMIN', 'SYSTEM', 'PROVIDER') AND actor_id <> ''),
    CONSTRAINT rwa_events_reason_check CHECK (reason_code IS NULL OR reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$')
);

CREATE TABLE rwa.reconciliation_runs (
    id UUID PRIMARY KEY,
    asset_id UUID NOT NULL REFERENCES rwa.assets(id) ON DELETE RESTRICT,
    chain_supply NUMERIC(38, 18) NOT NULL,
    locked_shares NUMERIC(38, 18) NOT NULL,
    difference NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL,
    compliance_case_id UUID REFERENCES compliance.cases(id) ON DELETE RESTRICT,
    observed_block TEXT NOT NULL,
    daily_key TEXT UNIQUE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT rwa_recon_whole_check CHECK (
        chain_supply >= 0 AND locked_shares >= 0
        AND chain_supply = trunc(chain_supply) AND locked_shares = trunc(locked_shares)
        AND difference = chain_supply - locked_shares
    ),
    CONSTRAINT rwa_recon_status_check CHECK (
        (status = 'BALANCED' AND difference = 0 AND compliance_case_id IS NULL)
        OR (status = 'DIFFERENCE' AND difference <> 0 AND compliance_case_id IS NOT NULL)
    ),
    CONSTRAINT rwa_recon_daily_key_check CHECK (
        daily_key IS NULL OR daily_key ~ '^[0-9a-f-]{36}:[0-9]{4}-[0-9]{2}-[0-9]{2}$'
    )
);

CREATE TABLE rwa.dividends (
    id UUID PRIMARY KEY,
    asset_id UUID NOT NULL REFERENCES rwa.assets(id) ON DELETE RESTRICT,
    external_reference TEXT NOT NULL UNIQUE,
    usd_per_share NUMERIC(38, 18) NOT NULL,
    record_at TIMESTAMPTZ NOT NULL,
    payable_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'ANNOUNCED',
    policy_version TEXT NOT NULL REFERENCES rwa.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT rwa_dividends_amount_check CHECK (usd_per_share > 0),
    CONSTRAINT rwa_dividends_timing_check CHECK (payable_at >= record_at),
    CONSTRAINT rwa_dividends_status_check CHECK (status IN (
        'ANNOUNCED', 'RECORD_DATE_LOCKED', 'ENTITLEMENT_CALCULATED', 'PAYMENT_RECEIVED',
        'WITHHOLDING_APPLIED', 'USD_CASH_CREDITED', 'RECONCILED'
    )),
    CONSTRAINT rwa_dividends_version_check CHECK (version > 0)
);

CREATE TABLE rwa.dividend_entitlements (
    id UUID PRIMARY KEY,
    dividend_id UUID NOT NULL REFERENCES rwa.dividends(id) ON DELETE RESTRICT,
    customer_reference TEXT NOT NULL REFERENCES rwa.customer_profiles(customer_reference) ON DELETE RESTRICT,
    whole_shares NUMERIC(38, 18) NOT NULL,
    gross_usd NUMERIC(38, 18) NOT NULL,
    withholding_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    net_usd NUMERIC(38, 18) NOT NULL,
    cash_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'CALCULATED',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT rwa_entitlements_shares_check CHECK (whole_shares > 0 AND whole_shares = trunc(whole_shares)),
    CONSTRAINT rwa_entitlements_amount_check CHECK (
        gross_usd > 0 AND withholding_usd >= 0 AND net_usd = gross_usd - withholding_usd AND net_usd >= 0
    ),
    CONSTRAINT rwa_entitlements_status_check CHECK (
        (status = 'CALCULATED' AND cash_ledger_transaction_id IS NULL)
        OR (status = 'USD_CASH_CREDITED' AND cash_ledger_transaction_id IS NOT NULL)
    ),
    UNIQUE (dividend_id, customer_reference)
);

CREATE TRIGGER rwa_provider_events_immutable
BEFORE UPDATE OR DELETE ON rwa.provider_events
FOR EACH ROW EXECUTE FUNCTION rwa.reject_immutable_mutation();

CREATE TRIGGER rwa_state_events_immutable
BEFORE UPDATE OR DELETE ON rwa.state_events
FOR EACH ROW EXECUTE FUNCTION rwa.reject_immutable_mutation();

CREATE TRIGGER rwa_reconciliation_runs_immutable
BEFORE UPDATE OR DELETE ON rwa.reconciliation_runs
FOR EACH ROW EXECUTE FUNCTION rwa.reject_immutable_mutation();

ALTER TABLE compliance.cases DROP CONSTRAINT compliance_cases_type_check;
ALTER TABLE compliance.cases ADD CONSTRAINT compliance_cases_type_check
CHECK (case_type IN (
    'EDD', 'BANK_WITHDRAWAL_REVIEW', 'TRANSACTION_MONITORING_ALERT',
    'CARD_DISPUTE', 'CARD_RECONCILIATION', 'RWA_ADDRESS_REVIEW', 'RWA_RECONCILIATION'
));

INSERT INTO rwa.policies (
    policy_version, chain_name, chain_id, custody_mode, address_cooling_hours,
    recovery_max_attempts, active, effective_at
) VALUES (
    'rwa-sim-v1', 'BASE_ANVIL', 84532, 'PLATFORM_VAULT', 24, 8, TRUE,
    TIMESTAMPTZ '2026-01-01 00:00:00+00'
);

INSERT INTO rwa.assets (
    id, instrument_id, underlying_symbol, token_name, token_symbol,
    contract_address, platform_vault_address, simulation_disclaimer, policy_version
) VALUES (
    '61c443ab-0d51-4cf2-9133-29308738f741',
    '8f3c34b4-264e-4ccf-92fb-074b0a4b6981',
    'AAPL', 'Cytisus AAPL RWA (Simulation)', 'CAAPL',
    '0x5FbDB2315678afecb367f032d93F642f64180aa3',
    '0x70997970C51812dc3A010C7d01b50e0d17dc79C8',
    'SIMULATION_ONLY_NOT_A_LEGALLY_ISSUED_SECURITY', 'rwa-sim-v1'
);

UPDATE marketdata.instrument_capabilities
SET rwa_mint_enabled = TRUE,
    policy_version = 'rwa-sim-v1'
WHERE instrument_id = '8f3c34b4-264e-4ccf-92fb-074b0a4b6981';

COMMENT ON SCHEMA rwa IS
    'Local single-chain RWA simulator. It makes no representation that a token is a legally issued security.';

COMMENT ON TABLE rwa.underlying_locks IS
    'Whole settled shares reserved one-for-one before chain mint. Release is allowed only after confirmed burn.';

COMMENT ON TABLE rwa.dividend_entitlements IS
    'Cash-only USD dividend entitlements. DRIP is intentionally unsupported.';
