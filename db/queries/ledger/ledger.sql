-- name: CreateLedgerAccount :one
INSERT INTO ledger.accounts (
    account_key,
    owner_type,
    owner_id,
    account_type,
    currency,
    normal_side
) VALUES (
    sqlc.arg(account_key),
    sqlc.arg(owner_type),
    sqlc.arg(owner_id),
    sqlc.arg(account_type),
    sqlc.arg(currency),
    sqlc.arg(normal_side)
)
RETURNING *;

-- name: GetLedgerAccount :one
SELECT *
FROM ledger.accounts
WHERE id = sqlc.arg(id);

-- name: InsertRequestIdempotency :one
INSERT INTO ledger.request_idempotency (
    scope,
    idempotency_key,
    request_hash,
    transaction_id
) VALUES (
    sqlc.arg(scope),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_hash),
    sqlc.arg(transaction_id)
)
ON CONFLICT (scope, idempotency_key) DO NOTHING
RETURNING transaction_id;

-- name: GetRequestIdempotency :one
SELECT *
FROM ledger.request_idempotency
WHERE scope = sqlc.arg(scope)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: InsertProviderEvent :one
INSERT INTO ledger.provider_events (
    provider,
    external_event_id,
    payload_hash,
    transaction_id
) VALUES (
    sqlc.arg(provider),
    sqlc.arg(external_event_id),
    sqlc.arg(payload_hash),
    sqlc.arg(transaction_id)
)
ON CONFLICT (provider, external_event_id) DO NOTHING
RETURNING transaction_id;

-- name: GetProviderEvent :one
SELECT *
FROM ledger.provider_events
WHERE provider = sqlc.arg(provider)
  AND external_event_id = sqlc.arg(external_event_id);

-- name: InsertLedgerTransaction :one
INSERT INTO ledger.transactions (
    id,
    transaction_type,
    policy_version,
    effective_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(transaction_type),
    sqlc.arg(policy_version),
    sqlc.arg(effective_at)
)
RETURNING *;

-- name: GetLedgerTransaction :one
SELECT *
FROM ledger.transactions
WHERE id = sqlc.arg(id);

-- name: InsertLedgerEntry :one
INSERT INTO ledger.entries (
    transaction_id,
    entry_sequence,
    account_id,
    currency,
    balance_dimension,
    direction,
    amount
) VALUES (
    sqlc.arg(transaction_id),
    sqlc.arg(entry_sequence),
    sqlc.arg(account_id),
    sqlc.arg(currency),
    sqlc.arg(balance_dimension),
    sqlc.arg(direction),
    sqlc.arg(amount)
)
RETURNING *;

-- name: ListLedgerEntriesForTransaction :many
SELECT *
FROM ledger.entries
WHERE transaction_id = sqlc.arg(transaction_id)
ORDER BY entry_sequence;

-- name: LockLedgerEntriesForTransaction :many
SELECT *
FROM ledger.entries
WHERE transaction_id = sqlc.arg(transaction_id)
ORDER BY entry_sequence
FOR SHARE;

-- name: InsertReversal :one
INSERT INTO ledger.reversals (
    original_transaction_id,
    reversal_transaction_id,
    reason_code
) VALUES (
    sqlc.arg(original_transaction_id),
    sqlc.arg(reversal_transaction_id),
    sqlc.arg(reason_code)
)
ON CONFLICT (original_transaction_id) DO NOTHING
RETURNING reversal_transaction_id;

-- name: GetReversal :one
SELECT *
FROM ledger.reversals
WHERE original_transaction_id = sqlc.arg(original_transaction_id);

-- name: GetAccountBalance :one
SELECT *
FROM ledger.account_balances
WHERE account_id = sqlc.arg(account_id)
  AND currency = sqlc.arg(currency)
  AND balance_dimension = sqlc.arg(balance_dimension);

-- name: InsertAuditEvent :one
INSERT INTO audit.events (
    action,
    resource_type,
    resource_id,
    actor_type,
    actor_id,
    correlation_id,
    metadata
) VALUES (
    sqlc.arg(action),
    sqlc.arg(resource_type),
    sqlc.arg(resource_id),
    sqlc.arg(actor_type),
    sqlc.arg(actor_id),
    sqlc.arg(correlation_id),
    sqlc.arg(metadata)
)
RETURNING *;

-- name: CountAuditEventsForCorrelation :one
SELECT COUNT(*)
FROM audit.events
WHERE correlation_id = sqlc.arg(correlation_id);
