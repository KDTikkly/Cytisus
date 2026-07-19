CREATE SCHEMA compliance;
CREATE SCHEMA banking;

CREATE FUNCTION compliance.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE compliance.cases (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL,
    case_type TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'OPEN',
    reason_code TEXT NOT NULL,
    next_action TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT compliance_cases_type_check
        CHECK (case_type IN ('EDD', 'BANK_WITHDRAWAL_REVIEW', 'TRANSACTION_MONITORING_ALERT')),
    CONSTRAINT compliance_cases_resource_check
        CHECK (resource_type <> '' AND resource_id <> ''),
    CONSTRAINT compliance_cases_status_check
        CHECK (status IN ('OPEN', 'INFORMATION_REQUIRED', 'IN_REVIEW', 'APPROVED', 'REJECTED', 'CLOSED')),
    CONSTRAINT compliance_cases_reason_check
        CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT compliance_cases_next_action_check
        CHECK (next_action <> ''),
    CONSTRAINT compliance_cases_policy_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT compliance_cases_version_check CHECK (version > 0)
);

CREATE UNIQUE INDEX compliance_cases_active_resource_idx
ON compliance.cases (case_type, resource_type, resource_id)
WHERE status NOT IN ('REJECTED', 'CLOSED');

CREATE FUNCTION compliance.enforce_case_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = NEW.status THEN
        RAISE EXCEPTION 'case status must change'
            USING ERRCODE = '23514';
    END IF;
    IF NOT (
        (OLD.status = 'OPEN' AND NEW.status IN ('INFORMATION_REQUIRED', 'IN_REVIEW', 'APPROVED', 'REJECTED'))
        OR (OLD.status = 'INFORMATION_REQUIRED' AND NEW.status IN ('IN_REVIEW', 'REJECTED'))
        OR (OLD.status = 'IN_REVIEW' AND NEW.status IN ('INFORMATION_REQUIRED', 'APPROVED', 'REJECTED'))
        OR (OLD.status IN ('APPROVED', 'REJECTED') AND NEW.status = 'CLOSED')
    ) THEN
        RAISE EXCEPTION 'invalid compliance case transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER compliance_cases_state_transition
BEFORE UPDATE ON compliance.cases
FOR EACH ROW EXECUTE FUNCTION compliance.enforce_case_transition();

CREATE TABLE compliance.case_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id UUID NOT NULL REFERENCES compliance.cases(id) ON DELETE RESTRICT,
    from_status TEXT,
    to_status TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT compliance_case_events_actor_check
        CHECK (actor_type IN ('USER', 'ADMIN', 'SYSTEM', 'PROVIDER') AND actor_id <> ''),
    CONSTRAINT compliance_case_events_reason_check
        CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT compliance_case_events_metadata_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE compliance.review_proposals (
    id UUID PRIMARY KEY,
    case_id UUID NOT NULL REFERENCES compliance.cases(id) ON DELETE RESTRICT,
    action TEXT NOT NULL,
    maker_id TEXT NOT NULL,
    maker_role TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    ticket_reference TEXT NOT NULL,
    evidence_reference TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    checker_id TEXT,
    checker_role TEXT,
    decision_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    decided_at TIMESTAMPTZ,
    CONSTRAINT compliance_review_action_check
        CHECK (action IN ('APPROVE_WITHDRAWAL', 'OVERRIDE_COOLING', 'REJECT_WITHDRAWAL')),
    CONSTRAINT compliance_review_maker_check
        CHECK (maker_id <> '' AND maker_role IN ('COMPLIANCE_ANALYST', 'RISK_ANALYST', 'OPERATIONS', 'ADMIN')),
    CONSTRAINT compliance_review_reason_check
        CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT compliance_review_evidence_check
        CHECK (ticket_reference <> '' AND evidence_reference <> ''),
    CONSTRAINT compliance_review_status_check
        CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    CONSTRAINT compliance_review_checker_check
        CHECK (
            (status = 'PENDING' AND checker_id IS NULL AND checker_role IS NULL AND decided_at IS NULL)
            OR (
                status IN ('APPROVED', 'REJECTED')
                AND checker_id IS NOT NULL
                AND checker_role IN ('COMPLIANCE_ANALYST', 'RISK_ANALYST', 'OPERATIONS', 'ADMIN')
                AND checker_id <> maker_id
                AND decision_reason IS NOT NULL
                AND decided_at IS NOT NULL
            )
        ),
    UNIQUE (case_id, action, status) DEFERRABLE INITIALLY DEFERRED
);

CREATE TRIGGER compliance_case_events_immutable
BEFORE UPDATE OR DELETE ON compliance.case_events
FOR EACH ROW EXECUTE FUNCTION compliance.reject_immutable_mutation();

CREATE FUNCTION banking.reject_immutable_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TABLE banking.customer_profiles (
    customer_reference TEXT PRIMARY KEY,
    paper_account_id UUID NOT NULL UNIQUE,
    cash_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    provider_clearing_ledger_account_id UUID NOT NULL REFERENCES ledger.accounts(id) ON DELETE RESTRICT,
    policy_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT banking_profiles_customer_check CHECK (customer_reference <> ''),
    CONSTRAINT banking_profiles_policy_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$')
);

CREATE TABLE banking.bank_accounts (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES banking.customer_profiles(customer_reference) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    external_account_reference TEXT NOT NULL,
    rail_support TEXT NOT NULL,
    owner_relation TEXT NOT NULL,
    ownership_status TEXT NOT NULL DEFAULT 'PENDING',
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    risk_class TEXT NOT NULL DEFAULT 'STANDARD',
    successfully_funded_at TIMESTAMPTZ,
    cooling_until TIMESTAMPTZ,
    policy_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT banking_accounts_provider_check
        CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT banking_accounts_external_check
        CHECK (external_account_reference ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{2,127}$'),
    CONSTRAINT banking_accounts_rail_check CHECK (rail_support IN ('ACH', 'WIRE', 'BOTH')),
    CONSTRAINT banking_accounts_owner_check CHECK (owner_relation IN ('SAME_NAME', 'THIRD_PARTY')),
    CONSTRAINT banking_accounts_ownership_check CHECK (ownership_status IN ('PENDING', 'VERIFIED', 'FAILED')),
    CONSTRAINT banking_accounts_third_party_check
        CHECK (owner_relation <> 'THIRD_PARTY' OR ownership_status <> 'VERIFIED'),
    CONSTRAINT banking_accounts_status_check CHECK (status IN ('ACTIVE', 'BLOCKED', 'CLOSED')),
    CONSTRAINT banking_accounts_risk_check CHECK (risk_class IN ('STANDARD', 'ELEVATED')),
    CONSTRAINT banking_accounts_cooling_check
        CHECK (cooling_until IS NULL OR owner_relation = 'SAME_NAME'),
    CONSTRAINT banking_accounts_policy_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT banking_accounts_version_check CHECK (version > 0),
    UNIQUE (customer_reference, provider, external_account_reference)
);

CREATE FUNCTION banking.enforce_bank_account_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.owner_relation <> NEW.owner_relation
        OR OLD.customer_reference <> NEW.customer_reference
        OR OLD.provider <> NEW.provider
        OR OLD.external_account_reference <> NEW.external_account_reference THEN
        RAISE EXCEPTION 'bank account ownership identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.ownership_status <> NEW.ownership_status AND NOT (
        (OLD.ownership_status = 'PENDING' AND NEW.ownership_status IN ('VERIFIED', 'FAILED'))
        OR (OLD.ownership_status = 'FAILED' AND NEW.ownership_status = 'PENDING')
    ) THEN
        RAISE EXCEPTION 'invalid ownership transition from % to %', OLD.ownership_status, NEW.ownership_status
            USING ERRCODE = '23514';
    END IF;
    IF OLD.status <> NEW.status AND NOT (
        (OLD.status = 'ACTIVE' AND NEW.status IN ('BLOCKED', 'CLOSED'))
        OR (OLD.status = 'BLOCKED' AND NEW.status IN ('ACTIVE', 'CLOSED'))
    ) THEN
        RAISE EXCEPTION 'invalid bank account transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER banking_accounts_state_transition
BEFORE UPDATE ON banking.bank_accounts
FOR EACH ROW EXECUTE FUNCTION banking.enforce_bank_account_transition();

CREATE TABLE banking.bank_account_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bank_account_id UUID NOT NULL REFERENCES banking.bank_accounts(id) ON DELETE RESTRICT,
    event_type TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT banking_account_events_type_check
        CHECK (event_type IN ('LINKED', 'OWNERSHIP_VERIFIED', 'OWNERSHIP_FAILED', 'FUNDING_SUCCEEDED', 'BLOCKED')),
    CONSTRAINT banking_account_events_actor_check
        CHECK (actor_type IN ('USER', 'ADMIN', 'SYSTEM', 'PROVIDER') AND actor_id <> ''),
    CONSTRAINT banking_account_events_metadata_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE banking.funding_transfers (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES banking.customer_profiles(customer_reference) ON DELETE RESTRICT,
    bank_account_id UUID NOT NULL REFERENCES banking.bank_accounts(id) ON DELETE RESTRICT,
    rail TEXT NOT NULL,
    amount NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL,
    provider TEXT NOT NULL,
    provider_transfer_id TEXT NOT NULL UNIQUE,
    pending_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    pending_release_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    settled_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    return_reversal_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    reason_code TEXT,
    policy_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT banking_funding_rail_check CHECK (rail IN ('ACH', 'WIRE')),
    CONSTRAINT banking_funding_amount_check CHECK (amount > 0),
    CONSTRAINT banking_funding_status_check CHECK (
        (rail = 'ACH' AND status IN ('INITIATED', 'PROCESSING', 'SETTLED', 'RETURNED', 'CANCELED'))
        OR (rail = 'WIRE' AND status IN (
            'INSTRUCTIONS_ISSUED', 'FUNDS_DETECTED', 'OWNERSHIP_REVIEW', 'CREDITED',
            'NAME_MISMATCH', 'MISSING_REFERENCE', 'THIRD_PARTY_FUNDS', 'RETURN_REQUIRED', 'MANUAL_REVIEW'
        ))
    ),
    CONSTRAINT banking_funding_provider_check
        CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT banking_funding_provider_id_check CHECK (provider_transfer_id <> ''),
    CONSTRAINT banking_funding_reason_check
        CHECK (reason_code IS NULL OR reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT banking_funding_policy_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT banking_funding_version_check CHECK (version > 0)
);

CREATE FUNCTION banking.enforce_funding_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = NEW.status THEN
        RAISE EXCEPTION 'funding status must change'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.rail = 'ACH' AND NOT (
        (OLD.status = 'INITIATED' AND NEW.status IN ('PROCESSING', 'SETTLED', 'RETURNED', 'CANCELED'))
        OR (OLD.status = 'PROCESSING' AND NEW.status IN ('SETTLED', 'RETURNED', 'CANCELED'))
        OR (OLD.status = 'SETTLED' AND NEW.status = 'RETURNED')
    ) THEN
        RAISE EXCEPTION 'invalid ACH transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    ELSIF OLD.rail = 'WIRE' AND NOT (
        (OLD.status = 'INSTRUCTIONS_ISSUED' AND NEW.status IN ('FUNDS_DETECTED', 'NAME_MISMATCH', 'MISSING_REFERENCE', 'THIRD_PARTY_FUNDS', 'MANUAL_REVIEW'))
        OR (OLD.status = 'FUNDS_DETECTED' AND NEW.status IN ('OWNERSHIP_REVIEW', 'CREDITED', 'NAME_MISMATCH', 'THIRD_PARTY_FUNDS', 'MANUAL_REVIEW'))
        OR (OLD.status = 'OWNERSHIP_REVIEW' AND NEW.status IN ('CREDITED', 'NAME_MISMATCH', 'THIRD_PARTY_FUNDS', 'MANUAL_REVIEW'))
        OR (OLD.status IN ('NAME_MISMATCH', 'MISSING_REFERENCE', 'THIRD_PARTY_FUNDS', 'MANUAL_REVIEW') AND NEW.status IN ('RETURN_REQUIRED', 'CREDITED'))
    ) THEN
        RAISE EXCEPTION 'invalid Wire transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER banking_funding_state_transition
BEFORE UPDATE ON banking.funding_transfers
FOR EACH ROW EXECUTE FUNCTION banking.enforce_funding_transition();

CREATE TABLE banking.funding_requests (
    customer_reference TEXT NOT NULL REFERENCES banking.customer_profiles(customer_reference) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    transfer_id UUID NOT NULL UNIQUE REFERENCES banking.funding_transfers(id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, idempotency_key),
    CONSTRAINT banking_funding_requests_key_check
        CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT banking_funding_requests_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE banking.withdrawals (
    id UUID PRIMARY KEY,
    customer_reference TEXT NOT NULL REFERENCES banking.customer_profiles(customer_reference) ON DELETE RESTRICT,
    bank_account_id UUID NOT NULL REFERENCES banking.bank_accounts(id) ON DELETE RESTRICT,
    amount NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    next_action TEXT NOT NULL,
    compliance_case_id UUID REFERENCES compliance.cases(id) ON DELETE RESTRICT,
    cooling_until TIMESTAMPTZ,
    reservation_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    submitted_ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    return_reservation_reversal_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    return_submitted_reversal_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    provider_transfer_id TEXT NOT NULL UNIQUE,
    policy_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT banking_withdrawal_amount_check CHECK (amount > 0),
    CONSTRAINT banking_withdrawal_status_check CHECK (status IN (
        'DRAFT', 'SECURITY_VERIFICATION', 'RISK_SCREENING', 'COOLING_OFF', 'APPROVED',
        'SUBMITTED_TO_BANK', 'PROCESSING', 'SETTLED', 'INFORMATION_REQUIRED', 'MANUAL_REVIEW',
        'SANCTIONS_HOLD', 'NAME_MISMATCH', 'REJECTED', 'RETURNED', 'CANCELED'
    )),
    CONSTRAINT banking_withdrawal_reason_check
        CHECK (reason_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT banking_withdrawal_next_action_check CHECK (next_action <> ''),
    CONSTRAINT banking_withdrawal_cooling_check
        CHECK ((status <> 'COOLING_OFF') OR cooling_until IS NOT NULL),
    CONSTRAINT banking_withdrawal_provider_check
        CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT banking_withdrawal_provider_id_check CHECK (provider_transfer_id <> ''),
    CONSTRAINT banking_withdrawal_policy_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT banking_withdrawal_version_check CHECK (version > 0)
);

CREATE FUNCTION banking.enforce_withdrawal_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = NEW.status THEN
        RAISE EXCEPTION 'withdrawal status must change'
            USING ERRCODE = '23514';
    END IF;
    IF NOT (
        (OLD.status = 'DRAFT' AND NEW.status IN ('SECURITY_VERIFICATION', 'RISK_SCREENING', 'REJECTED'))
        OR (OLD.status = 'SECURITY_VERIFICATION' AND NEW.status IN ('RISK_SCREENING', 'INFORMATION_REQUIRED', 'NAME_MISMATCH', 'REJECTED'))
        OR (OLD.status = 'INFORMATION_REQUIRED' AND NEW.status IN ('SECURITY_VERIFICATION', 'RISK_SCREENING', 'REJECTED', 'CANCELED'))
        OR (OLD.status = 'RISK_SCREENING' AND NEW.status IN ('COOLING_OFF', 'APPROVED', 'MANUAL_REVIEW', 'SANCTIONS_HOLD', 'NAME_MISMATCH', 'REJECTED'))
        OR (OLD.status = 'COOLING_OFF' AND NEW.status IN ('MANUAL_REVIEW', 'APPROVED', 'REJECTED', 'CANCELED'))
        OR (OLD.status = 'MANUAL_REVIEW' AND NEW.status IN ('INFORMATION_REQUIRED', 'APPROVED', 'REJECTED'))
        OR (OLD.status = 'SANCTIONS_HOLD' AND NEW.status IN ('MANUAL_REVIEW', 'REJECTED'))
        OR (OLD.status = 'APPROVED' AND NEW.status IN ('SUBMITTED_TO_BANK', 'CANCELED'))
        OR (OLD.status = 'SUBMITTED_TO_BANK' AND NEW.status IN ('PROCESSING', 'SETTLED', 'RETURNED'))
        OR (OLD.status = 'PROCESSING' AND NEW.status IN ('SETTLED', 'RETURNED'))
    ) THEN
        RAISE EXCEPTION 'invalid withdrawal transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;
    NEW.updated_at := clock_timestamp();
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER banking_withdrawal_state_transition
BEFORE UPDATE ON banking.withdrawals
FOR EACH ROW EXECUTE FUNCTION banking.enforce_withdrawal_transition();

CREATE TABLE banking.withdrawal_requests (
    customer_reference TEXT NOT NULL REFERENCES banking.customer_profiles(customer_reference) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    withdrawal_id UUID NOT NULL UNIQUE REFERENCES banking.withdrawals(id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (customer_reference, idempotency_key),
    CONSTRAINT banking_withdrawal_requests_key_check
        CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT banking_withdrawal_requests_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE banking.provider_events (
    provider TEXT NOT NULL,
    external_event_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    ledger_transaction_id UUID REFERENCES ledger.transactions(id) ON DELETE RESTRICT,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (provider, external_event_id),
    CONSTRAINT banking_provider_events_provider_check
        CHECK (provider ~ '^[a-z][a-z0-9._:-]{1,63}$'),
    CONSTRAINT banking_provider_events_external_check
        CHECK (external_event_id <> '' AND length(external_event_id) <= 255),
    CONSTRAINT banking_provider_events_resource_check
        CHECK (resource_type IN ('BANK_ACCOUNT', 'FUNDING', 'WITHDRAWAL')),
    CONSTRAINT banking_provider_events_type_check
        CHECK (event_type ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    CONSTRAINT banking_provider_events_hash_check CHECK (payload_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE banking.reconciliation_runs (
    id UUID PRIMARY KEY,
    scope TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'RUNNING',
    policy_version TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT banking_reconciliation_scope_check CHECK (scope IN ('CASH_VS_BANK_PROVIDER')),
    CONSTRAINT banking_reconciliation_status_check
        CHECK (status IN ('RUNNING', 'COMPLETED_WITHOUT_DIFFERENCE', 'COMPLETED_WITH_DIFFERENCES')),
    CONSTRAINT banking_reconciliation_completion_check
        CHECK ((status = 'RUNNING') = (completed_at IS NULL)),
    CONSTRAINT banking_reconciliation_policy_check
        CHECK (policy_version ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$')
);

CREATE TABLE banking.reconciliation_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES banking.reconciliation_runs(id) ON DELETE RESTRICT,
    customer_reference TEXT NOT NULL,
    ledger_amount NUMERIC(38, 18) NOT NULL,
    provider_amount NUMERIC(38, 18) NOT NULL,
    difference NUMERIC(38, 18) NOT NULL,
    status TEXT NOT NULL,
    compliance_case_id UUID REFERENCES compliance.cases(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT banking_reconciliation_item_status_check CHECK (status IN ('MATCHED', 'DIFFERENCE')),
    CONSTRAINT banking_reconciliation_item_difference_check
        CHECK ((status = 'MATCHED' AND difference = 0) OR (status = 'DIFFERENCE' AND difference <> 0)),
    UNIQUE (run_id, customer_reference)
);

CREATE TRIGGER banking_account_events_immutable
BEFORE UPDATE OR DELETE ON banking.bank_account_events
FOR EACH ROW EXECUTE FUNCTION banking.reject_immutable_mutation();

CREATE TRIGGER banking_funding_requests_immutable
BEFORE UPDATE OR DELETE ON banking.funding_requests
FOR EACH ROW EXECUTE FUNCTION banking.reject_immutable_mutation();

CREATE TRIGGER banking_withdrawal_requests_immutable
BEFORE UPDATE OR DELETE ON banking.withdrawal_requests
FOR EACH ROW EXECUTE FUNCTION banking.reject_immutable_mutation();

CREATE TRIGGER banking_provider_events_immutable
BEFORE UPDATE OR DELETE ON banking.provider_events
FOR EACH ROW EXECUTE FUNCTION banking.reject_immutable_mutation();

CREATE TRIGGER banking_reconciliation_items_immutable
BEFORE UPDATE OR DELETE ON banking.reconciliation_items
FOR EACH ROW EXECUTE FUNCTION banking.reject_immutable_mutation();

CREATE INDEX banking_bank_accounts_preferred_idx
ON banking.bank_accounts (customer_reference, successfully_funded_at DESC NULLS LAST, created_at);

CREATE INDEX banking_funding_customer_created_idx
ON banking.funding_transfers (customer_reference, created_at DESC, id DESC);

CREATE INDEX banking_withdrawal_customer_created_idx
ON banking.withdrawals (customer_reference, created_at DESC, id DESC);

COMMENT ON TABLE banking.provider_events IS
    'Immutable provider callback inbox. Adapters never mutate Ledger or balance projections directly.';

COMMENT ON COLUMN banking.bank_accounts.owner_relation IS
    'THIRD_PARTY is permanently ineligible for MVP withdrawals and can never become VERIFIED.';

COMMENT ON COLUMN banking.bank_accounts.cooling_until IS
    'Dynamic policy output. APIs expose the date and next action, never the internal risk weights or thresholds.';
