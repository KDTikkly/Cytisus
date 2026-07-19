CREATE TABLE securities.order_action_requests (
    paper_account_id UUID NOT NULL REFERENCES securities.paper_accounts(id) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    order_id UUID NOT NULL REFERENCES securities.orders(id) ON DELETE RESTRICT,
    action TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (paper_account_id, idempotency_key),
    CONSTRAINT securities_order_action_requests_key_check
        CHECK (idempotency_key ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$'),
    CONSTRAINT securities_order_action_requests_hash_check
        CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT securities_order_action_requests_action_check
        CHECK (action IN ('ADVANCE_REPLAY', 'CANCEL'))
);

CREATE TRIGGER securities_order_action_requests_immutable
BEFORE UPDATE OR DELETE ON securities.order_action_requests
FOR EACH ROW EXECUTE FUNCTION securities.reject_immutable_mutation();
