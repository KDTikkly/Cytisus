CREATE SCHEMA card;
CREATE SCHEMA notification;

CREATE FUNCTION card.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE card.policies (
    policy_version TEXT PRIMARY KEY,
    auth_fx_markup_rate NUMERIC(38, 18) NOT NULL,
    maximum_tip_rate NUMERIC(38, 18) NOT NULL,
    protected_limit_guardrail_rate NUMERIC(38, 18) NOT NULL,
    absolute_spending_cap_usd NUMERIC(38, 18) NOT NULL,
    provider_spending_cap_usd NUMERIC(38, 18) NOT NULL,
    default_daily_auto_sell_usd NUMERIC(38, 18) NOT NULL,
    quote_max_age_seconds INTEGER NOT NULL,
    statement_cycle_days INTEGER NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    active BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT card_policies_version_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT card_policies_rates_check CHECK (
        auth_fx_markup_rate >= 0 AND auth_fx_markup_rate < 1
        AND maximum_tip_rate >= 0 AND maximum_tip_rate <= 1
        AND protected_limit_guardrail_rate > 0 AND protected_limit_guardrail_rate < 1
    ),
    CONSTRAINT card_policies_limits_check CHECK (
        absolute_spending_cap_usd > 0
        AND provider_spending_cap_usd > 0
        AND default_daily_auto_sell_usd > 0
    ),
    CONSTRAINT card_policies_timing_check CHECK (quote_max_age_seconds > 0 AND statement_cycle_days BETWEEN 1 AND 31)
);

CREATE UNIQUE INDEX card_policies_one_active_idx
ON card.policies (active)
WHERE active;

CREATE TABLE card.customer_profiles (
    customer_reference TEXT PRIMARY KEY,
    paper_account_id UUID NOT NULL UNIQUE REFERENCES securities.paper_accounts(id) ON DELETE RESTRICT,
    cash_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    receivable_ledger_account_id UUID NOT NULL UNIQUE REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    provider_clearing_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    repayment_mode TEXT NOT NULL DEFAULT 'CASH_ONLY',
    spending_status TEXT NOT NULL DEFAULT 'ACTIVE',
    spending_status_reason TEXT,
    trust_tier TEXT NOT NULL DEFAULT 'STANDARD',
    kyc_status TEXT NOT NULL DEFAULT 'ELIGIBLE',
    policy_version TEXT NOT NULL REFERENCES card.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT card_profiles_customer_check CHECK (customer_reference <> ''),
    CONSTRAINT card_profiles_repayment_check
        CHECK (repayment_mode IN ('CASH_ONLY', 'CASH_THEN_AUTO_SELL', 'MONTHLY_STATEMENT')),
    CONSTRAINT card_profiles_spending_check CHECK (spending_status IN ('ACTIVE', 'FROZEN')),
    CONSTRAINT card_profiles_reason_check CHECK (
        (spending_status = 'ACTIVE' AND spending_status_reason IS NULL)
        OR (spending_status = 'FROZEN' AND spending_status_reason ~ '^[A-Z][A-Z0-9_]{2,63}$')
    ),
    CONSTRAINT card_profiles_trust_check CHECK (trust_tier IN ('STANDARD', 'ESTABLISHED')),
    CONSTRAINT card_profiles_kyc_check CHECK (kyc_status IN ('ELIGIBLE', 'REVIEW_REQUIRED', 'INELIGIBLE')),
    CONSTRAINT card_profiles_version_check CHECK (version > 0)
);

CREATE TABLE card.cards (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES card.customer_profiles(customer_reference) ON DELETE RESTRICT,
    card_type TEXT NOT NULL,
    status TEXT NOT NULL,
    display_name TEXT NOT NULL,
    last4 CHAR(4) NOT NULL,
    pin_set BOOLEAN NOT NULL DEFAULT FALSE,
    provider TEXT NOT NULL,
    provider_card_reference TEXT NOT NULL,
    apple_wallet_status TEXT NOT NULL DEFAULT 'UNAVAILABLE_SIMULATOR',
    google_wallet_status TEXT NOT NULL DEFAULT 'UNAVAILABLE_SIMULATOR',
    replacement_for_card_id UUID REFERENCES card.cards(id) ON DELETE RESTRICT,
    policy_version TEXT NOT NULL REFERENCES card.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT card_cards_type_check CHECK (card_type IN ('VIRTUAL', 'PLASTIC', 'METAL')),
    CONSTRAINT card_cards_status_check CHECK (
        status IN (
            'CREATED', 'ACTIVE', 'FROZEN', 'REPLACED', 'CLOSED',
            'APPLICATION_SUBMITTED', 'UNDER_REVIEW', 'APPROVED', 'MANUFACTURING',
            'SHIPPED', 'DELIVERED', 'ADDRESS_REVIEW', 'SHIPMENT_DELAYED',
            'REJECTED', 'LOST', 'STOLEN', 'REPLACEMENT_REQUESTED', 'CANCELED'
        )
    ),
    CONSTRAINT card_cards_type_status_check CHECK (
        (card_type = 'VIRTUAL' AND status IN ('CREATED', 'ACTIVE', 'FROZEN', 'REPLACED', 'CLOSED'))
        OR (card_type IN ('PLASTIC', 'METAL') AND status IN (
            'APPLICATION_SUBMITTED', 'UNDER_REVIEW', 'APPROVED', 'MANUFACTURING',
            'SHIPPED', 'DELIVERED', 'ACTIVE', 'FROZEN', 'ADDRESS_REVIEW',
            'SHIPMENT_DELAYED', 'REJECTED', 'LOST', 'STOLEN',
            'REPLACEMENT_REQUESTED', 'REPLACED', 'CANCELED', 'CLOSED'
        ))
    ),
    CONSTRAINT card_cards_last4_check CHECK (last4 ~ '^[0-9]{4}$'),
    CONSTRAINT card_cards_provider_check CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT card_cards_provider_reference_check CHECK (provider_card_reference <> ''),
    CONSTRAINT card_cards_wallet_check CHECK (
        apple_wallet_status IN ('UNAVAILABLE_SIMULATOR', 'NOT_ELIGIBLE', 'AVAILABLE', 'PROVISIONING', 'PROVISIONED', 'FAILED')
        AND google_wallet_status IN ('UNAVAILABLE_SIMULATOR', 'NOT_ELIGIBLE', 'AVAILABLE', 'PROVISIONING', 'PROVISIONED', 'FAILED')
    ),
    CONSTRAINT card_cards_version_check CHECK (version > 0),
    UNIQUE (provider, provider_card_reference)
);

CREATE UNIQUE INDEX card_one_open_virtual_per_customer_idx
ON card.cards (customer_reference)
WHERE card_type = 'VIRTUAL' AND status NOT IN ('REPLACED', 'CLOSED');

CREATE TABLE card.lifecycle_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id UUID NOT NULL REFERENCES card.cards(id) ON DELETE RESTRICT,
    from_status TEXT,
    to_status TEXT NOT NULL,
    action TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    reason_code TEXT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT card_lifecycle_action_check CHECK (action ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT card_lifecycle_actor_check
        CHECK (actor_type IN ('USER', 'ADMIN', 'SYSTEM', 'PROVIDER') AND actor_id <> ''),
    CONSTRAINT card_lifecycle_reason_check CHECK (reason_code IS NULL OR reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$')
);

CREATE TRIGGER card_lifecycle_events_immutable
BEFORE UPDATE OR DELETE ON card.lifecycle_events
FOR EACH ROW EXECUTE FUNCTION card.reject_immutable_mutation();

CREATE TABLE card.command_requests (
    customer_reference TEXT NOT NULL REFERENCES card.customer_profiles(customer_reference) ON DELETE RESTRICT,
    scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    resource_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, scope, idempotency_key),
    CONSTRAINT card_requests_scope_check CHECK (scope ~ '^[a-z][a-z0-9._:-]{2,127}$'),
    CONSTRAINT card_requests_key_check CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT card_requests_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE card.collateral_assets (
    symbol VARCHAR(12) PRIMARY KEY,
    display_name TEXT NOT NULL,
    asset_class TEXT NOT NULL,
    haircut_rate NUMERIC(38, 18) NOT NULL,
    reference_price_usd NUMERIC(38, 18) NOT NULL,
    quote_status TEXT NOT NULL DEFAULT 'SIMULATED',
    market_status TEXT NOT NULL DEFAULT 'OPEN',
    observed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    policy_version TEXT NOT NULL REFERENCES card.policies(policy_version) ON DELETE RESTRICT,
    CONSTRAINT card_collateral_symbol_check CHECK (symbol ~ '^[A-Z][A-Z0-9.]{0,11}$'),
    CONSTRAINT card_collateral_class_check CHECK (
        asset_class IN ('TREASURY_ETF', 'BROAD_MARKET_ETF', 'LARGE_CAP_STOCK', 'NORMAL_STOCK', 'HIGH_VOLATILITY_STOCK')
    ),
    CONSTRAINT card_collateral_value_check CHECK (haircut_rate > 0 AND haircut_rate < 1 AND reference_price_usd > 0),
    CONSTRAINT card_collateral_quote_check CHECK (quote_status IN ('SIMULATED', 'STALE', 'UNAVAILABLE')),
    CONSTRAINT card_collateral_market_check CHECK (market_status IN ('OPEN', 'CLOSED', 'HALTED'))
);

CREATE TABLE card.collateral_accounts (
    customer_reference TEXT NOT NULL REFERENCES card.customer_profiles(customer_reference) ON DELETE RESTRICT,
    symbol VARCHAR(12) NOT NULL REFERENCES card.collateral_assets(symbol) ON DELETE RESTRICT,
    source_type TEXT NOT NULL DEFAULT 'SIMULATED_LIVE',
    customer_ledger_account_id UUID NOT NULL UNIQUE REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    provider_inventory_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    frozen BOOLEAN NOT NULL DEFAULT FALSE,
    transferring BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, symbol),
    CONSTRAINT card_collateral_source_check CHECK (source_type = 'SIMULATED_LIVE')
);

CREATE TABLE card.auto_sell_mandates (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL UNIQUE REFERENCES card.customer_profiles(customer_reference) ON DELETE RESTRICT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    allow_fractional BOOLEAN NOT NULL DEFAULT TRUE,
    daily_max_usd NUMERIC(38, 18) NOT NULL,
    valid_until TIMESTAMPTZ NOT NULL,
    policy_version TEXT NOT NULL REFERENCES card.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT card_mandates_daily_check CHECK (daily_max_usd > 0),
    CONSTRAINT card_mandates_version_check CHECK (version > 0)
);

CREATE TABLE card.auto_sell_mandate_assets (
    mandate_id UUID NOT NULL REFERENCES card.auto_sell_mandates(id) ON DELETE RESTRICT,
    priority SMALLINT NOT NULL,
    symbol VARCHAR(12) NOT NULL REFERENCES card.collateral_assets(symbol) ON DELETE RESTRICT,
    minimum_retain_quantity NUMERIC(38, 18) NOT NULL DEFAULT 0,
    PRIMARY KEY (mandate_id, priority),
    UNIQUE (mandate_id, symbol),
    CONSTRAINT card_mandate_assets_priority_check CHECK (priority BETWEEN 1 AND 100),
    CONSTRAINT card_mandate_assets_retain_check CHECK (minimum_retain_quantity >= 0)
);

CREATE TABLE card.provider_events (
    provider TEXT NOT NULL,
    external_event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id UUID NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (provider, external_event_id),
    CONSTRAINT card_provider_events_provider_check CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT card_provider_events_external_check CHECK (external_event_id <> ''),
    CONSTRAINT card_provider_events_type_check CHECK (event_type ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT card_provider_events_hash_check CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT card_provider_events_resource_check CHECK (resource_type <> '')
);

CREATE TABLE card.authorizations (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES card.customer_profiles(customer_reference) ON DELETE RESTRICT,
    card_id UUID NOT NULL REFERENCES card.cards(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    external_authorization_id TEXT NOT NULL,
    merchant_name TEXT NOT NULL,
    merchant_category_code CHAR(4) NOT NULL,
    merchant_amount NUMERIC(38, 18) NOT NULL,
    merchant_currency CHAR(3) NOT NULL,
    authorization_fx_rate NUMERIC(38, 18) NOT NULL,
    fx_markup_rate NUMERIC(38, 18) NOT NULL,
    authorized_usd NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL,
    offline BOOLEAN NOT NULL DEFAULT FALSE,
    entry_mode TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    decline_code TEXT,
    hold_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    captured_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    reversed_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    policy_version TEXT NOT NULL REFERENCES card.policies(policy_version) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT card_authorizations_provider_check CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT card_authorizations_external_check CHECK (external_authorization_id <> ''),
    CONSTRAINT card_authorizations_merchant_check CHECK (merchant_name <> '' AND merchant_category_code ~ '^[0-9]{4}$'),
    CONSTRAINT card_authorizations_currency_check CHECK (merchant_currency ~ '^[A-Z]{3}$'),
    CONSTRAINT card_authorizations_amount_check CHECK (
        merchant_amount > 0 AND authorization_fx_rate > 0 AND fx_markup_rate >= 0
        AND authorized_usd > 0 AND captured_usd >= 0 AND reversed_usd >= 0
    ),
    CONSTRAINT card_authorizations_status_check CHECK (
        status IN ('APPROVED', 'PARTIALLY_CAPTURED', 'CAPTURED', 'REVERSED', 'DECLINED')
    ),
    CONSTRAINT card_authorizations_entry_mode_check CHECK (
        entry_mode IN ('NFC_SIMULATOR', 'ECOMMERCE_SIMULATOR', 'MANUAL_SIMULATOR', 'OFFLINE_SIMULATOR')
    ),
    CONSTRAINT card_authorizations_decline_check CHECK (
        (status = 'DECLINED' AND decline_code ~ '^[A-Z][A-Z0-9_]{2,63}$' AND hold_ledger_transaction_id IS NULL)
        OR (status <> 'DECLINED' AND decline_code IS NULL AND hold_ledger_transaction_id IS NOT NULL)
    ),
    CONSTRAINT card_authorizations_version_check CHECK (version > 0),
    UNIQUE (provider, external_authorization_id)
);

CREATE TABLE card.captures (
    id UUID PRIMARY KEY,
    authorization_id UUID NOT NULL REFERENCES card.authorizations(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    external_capture_id TEXT NOT NULL,
    merchant_amount NUMERIC(38, 18) NOT NULL,
    merchant_currency CHAR(3) NOT NULL,
    clearing_fx_rate NUMERIC(38, 18) NOT NULL,
    fx_markup_rate NUMERIC(38, 18) NOT NULL,
    settled_usd NUMERIC(38, 18) NOT NULL,
    tip_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    hold_released_usd NUMERIC(38, 18) NOT NULL,
    cash_repaid_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    auto_sell_repaid_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    refunded_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'CAPTURED',
    receivable_ledger_transaction_id UUID NOT NULL REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    repayment_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT card_captures_provider_check CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT card_captures_external_check CHECK (external_capture_id <> ''),
    CONSTRAINT card_captures_currency_check CHECK (merchant_currency ~ '^[A-Z]{3}$'),
    CONSTRAINT card_captures_amount_check CHECK (
        merchant_amount > 0 AND clearing_fx_rate > 0 AND fx_markup_rate >= 0 AND settled_usd > 0
        AND tip_usd >= 0 AND hold_released_usd >= 0 AND cash_repaid_usd >= 0
        AND auto_sell_repaid_usd >= 0 AND refunded_usd >= 0
        AND cash_repaid_usd + auto_sell_repaid_usd <= settled_usd
        AND refunded_usd <= settled_usd
    ),
    CONSTRAINT card_captures_status_check CHECK (status IN ('CAPTURED', 'PARTIALLY_REFUNDED', 'REFUNDED', 'DISPUTED')),
    UNIQUE (provider, external_capture_id)
);

CREATE TABLE card.reversals (
    id UUID PRIMARY KEY,
    authorization_id UUID NOT NULL REFERENCES card.authorizations(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    external_reversal_id TEXT NOT NULL,
    reversed_usd NUMERIC(38, 18) NOT NULL,
    ledger_transaction_id UUID NOT NULL REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT card_reversals_amount_check CHECK (reversed_usd > 0),
    UNIQUE (provider, external_reversal_id)
);

CREATE TABLE card.refunds (
    id UUID PRIMARY KEY,
    capture_id UUID NOT NULL REFERENCES card.captures(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    external_refund_id TEXT NOT NULL,
    refund_usd NUMERIC(38, 18) NOT NULL,
    receivable_reduction_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    cash_credit_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    ledger_transaction_id UUID NOT NULL REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT card_refunds_amount_check CHECK (
        refund_usd > 0 AND receivable_reduction_usd >= 0 AND cash_credit_usd >= 0
        AND receivable_reduction_usd + cash_credit_usd = refund_usd
    ),
    UNIQUE (provider, external_refund_id)
);

CREATE TABLE card.disputes (
    id UUID PRIMARY KEY,
    capture_id UUID NOT NULL REFERENCES card.captures(id) ON DELETE RESTRICT,
    customer_reference TEXT NOT NULL REFERENCES card.customer_profiles(customer_reference) ON DELETE RESTRICT,
    amount_usd NUMERIC(38, 18) NOT NULL,
    reason_code TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'OPEN',
    outcome TEXT,
    compliance_case_id UUID REFERENCES compliance.cases(id) ON DELETE RESTRICT,
    resolution_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    opened_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    resolved_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT card_disputes_amount_check CHECK (amount_usd > 0),
    CONSTRAINT card_disputes_reason_check CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT card_disputes_status_check CHECK (status IN ('OPEN', 'UNDER_REVIEW', 'RESOLVED')),
    CONSTRAINT card_disputes_outcome_check CHECK (
        (status <> 'RESOLVED' AND outcome IS NULL AND resolved_at IS NULL)
        OR (status = 'RESOLVED' AND outcome IN ('ACCEPTED', 'REJECTED') AND resolved_at IS NOT NULL)
    ),
    CONSTRAINT card_disputes_version_check CHECK (version > 0)
);

CREATE TABLE card.auto_sell_executions (
    id UUID PRIMARY KEY,
    capture_id UUID NOT NULL REFERENCES card.captures(id) DEFERRABLE INITIALLY DEFERRED,
    mandate_id UUID NOT NULL REFERENCES card.auto_sell_mandates(id) ON DELETE RESTRICT,
    execution_sequence SMALLINT NOT NULL,
    symbol VARCHAR(12) NOT NULL REFERENCES card.collateral_assets(symbol) ON DELETE RESTRICT,
    requested_quantity NUMERIC(38, 18) NOT NULL,
    protected_limit_price NUMERIC(38, 18) NOT NULL,
    execution_price NUMERIC(38, 18) NOT NULL DEFAULT 0,
    filled_quantity NUMERIC(38, 18) NOT NULL DEFAULT 0,
    proceeds_usd NUMERIC(38, 18) NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    failure_code TEXT,
    ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT card_auto_sell_sequence_check CHECK (execution_sequence BETWEEN 1 AND 100),
    CONSTRAINT card_auto_sell_amount_check CHECK (
        requested_quantity > 0 AND protected_limit_price > 0 AND execution_price >= 0
        AND filled_quantity >= 0 AND proceeds_usd >= 0
    ),
    CONSTRAINT card_auto_sell_status_check CHECK (status IN ('FILLED', 'PARTIALLY_FILLED', 'FAILED')),
    CONSTRAINT card_auto_sell_protection_check CHECK (
        status = 'FAILED' OR (execution_price >= protected_limit_price AND ledger_transaction_id IS NOT NULL)
    ),
    CONSTRAINT card_auto_sell_failure_check CHECK (
        (status = 'FAILED' AND failure_code ~ '^[A-Z][A-Z0-9_]{2,63}$')
        OR (status <> 'FAILED' AND failure_code IS NULL)
    ),
    UNIQUE (capture_id, execution_sequence)
);

CREATE TABLE card.statements (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES card.customer_profiles(customer_reference) ON DELETE RESTRICT,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    amount_due_usd NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL DEFAULT 'OPEN',
    due_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ,
    repayment_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT card_statements_period_check CHECK (period_end >= period_start),
    CONSTRAINT card_statements_amount_check CHECK (amount_due_usd >= 0),
    CONSTRAINT card_statements_status_check CHECK (status IN ('OPEN', 'PAID', 'PAST_DUE')),
    CONSTRAINT card_statements_payment_check CHECK (
        (status = 'PAID' AND paid_at IS NOT NULL AND repayment_ledger_transaction_id IS NOT NULL)
        OR (status <> 'PAID' AND paid_at IS NULL AND repayment_ledger_transaction_id IS NULL)
    ),
    UNIQUE (customer_reference, period_start, period_end)
);

CREATE TABLE card.reconciliation_runs (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES card.customer_profiles(customer_reference) ON DELETE RESTRICT,
    ledger_hold_usd NUMERIC(38, 18) NOT NULL,
    provider_hold_usd NUMERIC(38, 18) NOT NULL,
    ledger_receivable_usd NUMERIC(38, 18) NOT NULL,
    provider_receivable_usd NUMERIC(38, 18) NOT NULL,
    difference_usd NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL,
    compliance_case_id UUID REFERENCES compliance.cases(id) ON DELETE RESTRICT,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT card_reconciliation_amount_check CHECK (
        ledger_hold_usd >= 0 AND provider_hold_usd >= 0
        AND ledger_receivable_usd >= 0 AND provider_receivable_usd >= 0
    ),
    CONSTRAINT card_reconciliation_status_check
        CHECK (status IN ('COMPLETED_WITHOUT_DIFFERENCE', 'COMPLETED_WITH_DIFFERENCES'))
);

CREATE FUNCTION notification.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE notification.events (
    id UUID PRIMARY KEY,
    notification_event_id TEXT NOT NULL UNIQUE,
    customer_reference TEXT NOT NULL,
    category TEXT NOT NULL,
    event_type TEXT NOT NULL,
    title_key TEXT NOT NULL,
    body_key TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    action_path TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT notification_events_identity_check CHECK (notification_event_id <> ''),
    CONSTRAINT notification_events_category_check CHECK (category IN ('CARD', 'SECURITY', 'BANKING', 'TRADING')),
    CONSTRAINT notification_events_type_check CHECK (event_type ~ '^[a-z][a-z0-9._-]{2,127}$'),
    CONSTRAINT notification_events_copy_check CHECK (title_key <> '' AND body_key <> ''),
    CONSTRAINT notification_events_resource_check CHECK (resource_type <> '' AND resource_id <> '')
);

CREATE TABLE notification.deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES notification.events(id) ON DELETE RESTRICT,
    channel TEXT NOT NULL,
    provider TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'QUEUED',
    failure_code TEXT,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT notification_deliveries_channel_check CHECK (channel IN ('IN_APP', 'PUSH', 'EMAIL')),
    CONSTRAINT notification_deliveries_provider_check CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT notification_deliveries_status_check CHECK (status IN ('QUEUED', 'SENT', 'FAILED')),
    CONSTRAINT notification_deliveries_failure_check CHECK (
        (status = 'FAILED' AND failure_code ~ '^[A-Z][A-Z0-9_]{2,63}$')
        OR (status <> 'FAILED' AND failure_code IS NULL)
    ),
    UNIQUE (event_id, channel)
);

CREATE TRIGGER notification_events_immutable
BEFORE UPDATE OR DELETE ON notification.events
FOR EACH ROW EXECUTE FUNCTION notification.reject_immutable_mutation();

CREATE TRIGGER notification_deliveries_no_delete
BEFORE DELETE ON notification.deliveries
FOR EACH ROW EXECUTE FUNCTION notification.reject_immutable_mutation();

CREATE TRIGGER card_provider_events_immutable
BEFORE UPDATE OR DELETE ON card.provider_events
FOR EACH ROW EXECUTE FUNCTION card.reject_immutable_mutation();

CREATE TRIGGER card_reversals_immutable
BEFORE UPDATE OR DELETE ON card.reversals
FOR EACH ROW EXECUTE FUNCTION card.reject_immutable_mutation();

CREATE TRIGGER card_refunds_immutable
BEFORE UPDATE OR DELETE ON card.refunds
FOR EACH ROW EXECUTE FUNCTION card.reject_immutable_mutation();

ALTER TABLE compliance.cases DROP CONSTRAINT compliance_cases_type_check;
ALTER TABLE compliance.cases ADD CONSTRAINT compliance_cases_type_check
CHECK (case_type IN (
    'EDD', 'BANK_WITHDRAWAL_REVIEW', 'TRANSACTION_MONITORING_ALERT',
    'CARD_DISPUTE', 'CARD_RECONCILIATION'
));

INSERT INTO card.policies (
    policy_version,
    auth_fx_markup_rate,
    maximum_tip_rate,
    protected_limit_guardrail_rate,
    absolute_spending_cap_usd,
    provider_spending_cap_usd,
    default_daily_auto_sell_usd,
    quote_max_age_seconds,
    statement_cycle_days,
    effective_at,
    active
) VALUES (
    'card-sim-v1',
    0.01,
    0.30,
    0.02,
    250000,
    250000,
    25000,
    300,
    30,
    TIMESTAMPTZ '2026-01-01 00:00:00+00',
    TRUE
);

INSERT INTO card.collateral_assets (
    symbol, display_name, asset_class, haircut_rate, reference_price_usd,
    quote_status, market_status, observed_at, policy_version
) VALUES
    ('TLT', 'iShares 20+ Year Treasury Bond ETF', 'TREASURY_ETF', 0.85, 90, 'SIMULATED', 'OPEN', clock_timestamp(), 'card-sim-v1'),
    ('VTI', 'Vanguard Total Stock Market ETF', 'BROAD_MARKET_ETF', 0.75, 300, 'SIMULATED', 'OPEN', clock_timestamp(), 'card-sim-v1'),
    ('AAPL', 'Apple Inc.', 'LARGE_CAP_STOCK', 0.60, 220, 'SIMULATED', 'OPEN', clock_timestamp(), 'card-sim-v1'),
    ('XYZ', 'Synthetic High Volatility Stock', 'HIGH_VOLATILITY_STOCK', 0.20, 25, 'SIMULATED', 'OPEN', clock_timestamp(), 'card-sim-v1');
