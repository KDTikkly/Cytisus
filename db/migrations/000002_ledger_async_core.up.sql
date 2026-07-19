CREATE SCHEMA ledger;
CREATE SCHEMA audit;
CREATE SCHEMA outbox;

CREATE FUNCTION ledger.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE ledger.accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_key TEXT NOT NULL UNIQUE,
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    account_type TEXT NOT NULL,
    currency VARCHAR(12) NOT NULL,
    normal_side TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT ledger_accounts_key_check
        CHECK (account_key ~ '^[a-z][a-z0-9._:-]{2,127}$'),
    CONSTRAINT ledger_accounts_owner_type_check
        CHECK (owner_type IN ('USER', 'PLATFORM', 'PROVIDER', 'SYSTEM')),
    CONSTRAINT ledger_accounts_owner_id_check
        CHECK (owner_id <> ''),
    CONSTRAINT ledger_accounts_type_check
        CHECK (account_type IN (
            'CRYPTO',
            'CASH',
            'SECURITIES',
            'CARD_RECEIVABLE',
            'PLATFORM_FEE',
            'PROVIDER_CLEARING',
            'SUSPENSE',
            'FROZEN_FUNDS',
            'RWA_LOCKED'
        )),
    CONSTRAINT ledger_accounts_currency_check
        CHECK (currency ~ '^[A-Z][A-Z0-9]{2,11}$'),
    CONSTRAINT ledger_accounts_normal_side_check
        CHECK (normal_side IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_accounts_version_check
        CHECK (version > 0),
    UNIQUE (id, currency)
);

CREATE TRIGGER ledger_accounts_immutable
BEFORE UPDATE OR DELETE ON ledger.accounts
FOR EACH ROW EXECUTE FUNCTION ledger.reject_immutable_mutation();

CREATE TABLE ledger.transactions (
    id UUID PRIMARY KEY,
    transaction_type TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ledger_transactions_type_check
        CHECK (transaction_type ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT ledger_transactions_policy_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$')
);

CREATE TRIGGER ledger_transactions_immutable
BEFORE UPDATE OR DELETE ON ledger.transactions
FOR EACH ROW EXECUTE FUNCTION ledger.reject_immutable_mutation();

CREATE TABLE ledger.entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    entry_sequence SMALLINT NOT NULL,
    account_id UUID NOT NULL,
    currency VARCHAR(12) NOT NULL,
    balance_dimension TEXT NOT NULL,
    direction TEXT NOT NULL,
    amount NUMERIC(38, 18) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ledger_entries_sequence_check
        CHECK (entry_sequence > 0),
    CONSTRAINT ledger_entries_currency_check
        CHECK (currency ~ '^[A-Z][A-Z0-9]{2,11}$'),
    CONSTRAINT ledger_entries_dimension_check
        CHECK (balance_dimension IN (
            'PENDING',
            'HELD',
            'SETTLED',
            'WITHDRAWABLE',
            'PROVISIONAL_BUYING_POWER',
            'CARD_SPENDABLE',
            'FROZEN',
            'RECEIVABLE'
        )),
    CONSTRAINT ledger_entries_direction_check
        CHECK (direction IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_entries_amount_check
        CHECK (amount > 0),
    CONSTRAINT ledger_entries_account_currency_fk
        FOREIGN KEY (account_id, currency)
        REFERENCES ledger.accounts(id, currency)
        ON DELETE RESTRICT,
    UNIQUE (transaction_id, entry_sequence)
);

CREATE INDEX ledger_entries_account_projection_idx
    ON ledger.entries (account_id, currency, balance_dimension, created_at, id);

CREATE TRIGGER ledger_entries_immutable
BEFORE UPDATE OR DELETE ON ledger.entries
FOR EACH ROW EXECUTE FUNCTION ledger.reject_immutable_mutation();

CREATE FUNCTION ledger.enforce_balanced_transaction()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_transaction_id UUID;
BEGIN
    IF TG_TABLE_NAME = 'transactions' THEN
        target_transaction_id := NEW.id;
    ELSE
        target_transaction_id := NEW.transaction_id;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM ledger.entries AS entry
        WHERE entry.transaction_id = target_transaction_id
    ) OR EXISTS (
        SELECT 1
        FROM ledger.entries AS entry
        WHERE entry.transaction_id = target_transaction_id
        GROUP BY entry.currency
        HAVING COUNT(*) < 2
            OR COALESCE(SUM(entry.amount) FILTER (WHERE entry.direction = 'DEBIT'), 0)
                <> COALESCE(SUM(entry.amount) FILTER (WHERE entry.direction = 'CREDIT'), 0)
    ) THEN
        RAISE EXCEPTION 'ledger transaction % is not balanced', target_transaction_id
            USING ERRCODE = '23514';
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER ledger_transactions_balanced
AFTER INSERT ON ledger.transactions
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION ledger.enforce_balanced_transaction();

CREATE CONSTRAINT TRIGGER ledger_entries_balanced
AFTER INSERT ON ledger.entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION ledger.enforce_balanced_transaction();

CREATE TABLE ledger.reversals (
    original_transaction_id UUID PRIMARY KEY
        REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    reversal_transaction_id UUID NOT NULL UNIQUE
        REFERENCES ledger.transactions(id) DEFERRABLE INITIALLY DEFERRED,
    reason_code TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ledger_reversals_distinct_transaction_check
        CHECK (original_transaction_id <> reversal_transaction_id),
    CONSTRAINT ledger_reversals_reason_check
        CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$')
);

CREATE TRIGGER ledger_reversals_immutable
BEFORE UPDATE OR DELETE ON ledger.reversals
FOR EACH ROW EXECUTE FUNCTION ledger.reject_immutable_mutation();

CREATE TABLE ledger.request_idempotency (
    scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    transaction_id UUID NOT NULL UNIQUE
        REFERENCES ledger.transactions(id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (scope, idempotency_key),
    CONSTRAINT ledger_request_idempotency_scope_check
        CHECK (scope ~ '^[a-z][a-z0-9._:-]{2,127}$'),
    CONSTRAINT ledger_request_idempotency_key_check
        CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT ledger_request_idempotency_hash_check
        CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TRIGGER ledger_request_idempotency_immutable
BEFORE UPDATE OR DELETE ON ledger.request_idempotency
FOR EACH ROW EXECUTE FUNCTION ledger.reject_immutable_mutation();

CREATE TABLE ledger.provider_events (
    provider TEXT NOT NULL,
    external_event_id TEXT NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    transaction_id UUID NOT NULL
        REFERENCES ledger.transactions(id) DEFERRABLE INITIALLY DEFERRED,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (provider, external_event_id),
    CONSTRAINT ledger_provider_events_provider_check
        CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT ledger_provider_events_external_id_check
        CHECK (external_event_id <> '' AND length(external_event_id) <= 255),
    CONSTRAINT ledger_provider_events_hash_check
        CHECK (payload_hash ~ '^[0-9a-f]{64}$')
);

CREATE TRIGGER ledger_provider_events_immutable
BEFORE UPDATE OR DELETE ON ledger.provider_events
FOR EACH ROW EXECUTE FUNCTION ledger.reject_immutable_mutation();

CREATE TABLE ledger.holds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL,
    currency VARCHAR(12) NOT NULL,
    amount NUMERIC(38, 18) NOT NULL,
    reason_code TEXT NOT NULL,
    created_transaction_id UUID NOT NULL UNIQUE
        REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ledger_holds_account_currency_fk
        FOREIGN KEY (account_id, currency)
        REFERENCES ledger.accounts(id, currency)
        ON DELETE RESTRICT,
    CONSTRAINT ledger_holds_amount_check
        CHECK (amount > 0),
    CONSTRAINT ledger_holds_reason_check
        CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT ledger_holds_expiry_check
        CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE TRIGGER ledger_holds_immutable
BEFORE UPDATE OR DELETE ON ledger.holds
FOR EACH ROW EXECUTE FUNCTION ledger.reject_immutable_mutation();

CREATE TABLE ledger.hold_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hold_id UUID NOT NULL REFERENCES ledger.holds(id) ON DELETE RESTRICT,
    event_type TEXT NOT NULL,
    transaction_id UUID NOT NULL REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ledger_hold_events_type_check
        CHECK (event_type IN ('CREATED', 'RELEASED', 'CAPTURED', 'EXPIRED', 'CANCELLED')),
    UNIQUE (hold_id, event_type),
    UNIQUE (hold_id, transaction_id)
);

CREATE TRIGGER ledger_hold_events_immutable
BEFORE UPDATE OR DELETE ON ledger.hold_events
FOR EACH ROW EXECUTE FUNCTION ledger.reject_immutable_mutation();

CREATE VIEW ledger.account_balances AS
SELECT
    entry.account_id,
    entry.currency,
    entry.balance_dimension,
    CAST(SUM(
        CASE entry.direction
            WHEN 'DEBIT' THEN entry.amount
            ELSE -entry.amount
        END
    ) AS NUMERIC(38, 18)) AS debit_balance,
    MAX(entry.created_at) AS last_entry_at
FROM ledger.entries AS entry
GROUP BY entry.account_id, entry.currency, entry.balance_dimension;

CREATE FUNCTION audit.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE audit.events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    correlation_id UUID,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT audit_events_action_check
        CHECK (action ~ '^[a-z][a-z0-9._:-]{2,127}$'),
    CONSTRAINT audit_events_resource_check
        CHECK (resource_type <> '' AND resource_id <> ''),
    CONSTRAINT audit_events_actor_check
        CHECK (actor_type IN ('USER', 'ADMIN', 'SYSTEM', 'PROVIDER') AND actor_id <> ''),
    CONSTRAINT audit_events_metadata_object_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX audit_events_resource_idx
    ON audit.events (resource_type, resource_id, occurred_at DESC);

CREATE TRIGGER audit_events_immutable
BEFORE UPDATE OR DELETE ON audit.events
FOR EACH ROW EXECUTE FUNCTION audit.reject_immutable_mutation();

CREATE FUNCTION outbox.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE outbox.events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    event_version INTEGER NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 8,
    available_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    claimed_by TEXT,
    claimed_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    last_error TEXT,
    cancelled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT outbox_events_aggregate_check
        CHECK (aggregate_type <> '' AND aggregate_id <> ''),
    CONSTRAINT outbox_events_type_check
        CHECK (event_type ~ '^[a-z][a-z0-9._:-]{2,127}$'),
    CONSTRAINT outbox_events_version_check
        CHECK (event_version > 0),
    CONSTRAINT outbox_events_payload_object_check
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT outbox_events_status_check
        CHECK (status IN ('PENDING', 'PROCESSING', 'DELIVERED', 'DEAD_LETTER', 'CANCELLED')),
    CONSTRAINT outbox_events_attempts_check
        CHECK (attempt_count >= 0 AND max_attempts > 0 AND attempt_count <= max_attempts),
    CONSTRAINT outbox_events_claim_check
        CHECK ((claimed_by IS NULL) = (claimed_at IS NULL)),
    CONSTRAINT outbox_events_delivered_check
        CHECK (status <> 'DELIVERED' OR delivered_at IS NOT NULL),
    CONSTRAINT outbox_events_cancelled_check
        CHECK (status <> 'CANCELLED' OR cancelled_at IS NOT NULL),
    UNIQUE (aggregate_type, aggregate_id, event_type, event_version)
);

CREATE INDEX outbox_events_claim_idx
    ON outbox.events (available_at, created_at, id)
    WHERE status IN ('PENDING', 'PROCESSING');

CREATE TABLE outbox.consumer_receipts (
    consumer_name TEXT NOT NULL,
    event_id UUID NOT NULL REFERENCES outbox.events(id) ON DELETE RESTRICT,
    handled_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (consumer_name, event_id),
    CONSTRAINT outbox_consumer_receipts_name_check
        CHECK (consumer_name ~ '^[a-z][a-z0-9._:-]{2,127}$')
);

CREATE TRIGGER outbox_consumer_receipts_immutable
BEFORE UPDATE OR DELETE ON outbox.consumer_receipts
FOR EACH ROW EXECUTE FUNCTION outbox.reject_immutable_mutation();

COMMENT ON VIEW ledger.account_balances IS
    'Read-only projection derived exclusively from immutable ledger entries; never update balances directly.';

COMMENT ON TABLE outbox.consumer_receipts IS
    'Consumer idempotency receipts. A replay may redeliver an event but cannot repeat a recorded consumer effect.';
