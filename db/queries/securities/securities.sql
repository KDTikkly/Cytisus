-- name: LockRegistrationFixture :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(fixture_id), 0));

-- name: LockPaperAccount :one
SELECT id
FROM securities.paper_accounts
WHERE id = sqlc.arg(id)
  AND status = 'ACTIVE'
FOR UPDATE;

-- name: InsertPaperAccount :one
INSERT INTO securities.paper_accounts (
    id,
    fixture_id,
    customer_reference,
    session_token_hash,
    cash_ledger_account_id,
    funding_ledger_account_id,
    initial_cash
) VALUES (
    sqlc.arg(id),
    sqlc.arg(fixture_id),
    sqlc.arg(customer_reference),
    sqlc.arg(session_token_hash),
    sqlc.arg(cash_ledger_account_id),
    sqlc.arg(funding_ledger_account_id),
    sqlc.arg(initial_cash)
)
RETURNING *;

-- name: GetPaperAccountByFixture :one
SELECT *
FROM securities.paper_accounts
WHERE fixture_id = sqlc.arg(fixture_id);

-- name: GetPaperAccountBySessionHash :one
SELECT *
FROM securities.paper_accounts
WHERE session_token_hash = sqlc.arg(session_token_hash)
  AND status = 'ACTIVE';

-- name: GetPaperAccount :one
SELECT *
FROM securities.paper_accounts
WHERE id = sqlc.arg(id);

-- name: GetInstrumentLedgerAccounts :one
SELECT *
FROM securities.instrument_ledger_accounts
WHERE paper_account_id = sqlc.arg(paper_account_id)
  AND instrument_id = sqlc.arg(instrument_id);

-- name: InsertInstrumentLedgerAccounts :one
INSERT INTO securities.instrument_ledger_accounts (
    paper_account_id,
    instrument_id,
    symbol,
    customer_ledger_account_id,
    broker_inventory_ledger_account_id
) VALUES (
    sqlc.arg(paper_account_id),
    sqlc.arg(instrument_id),
    sqlc.arg(symbol),
    sqlc.arg(customer_ledger_account_id),
    sqlc.arg(broker_inventory_ledger_account_id)
)
ON CONFLICT (paper_account_id, instrument_id) DO NOTHING
RETURNING *;

-- name: InsertOrderRequest :one
INSERT INTO securities.order_requests (
    paper_account_id,
    idempotency_key,
    request_hash,
    order_id
) VALUES (
    sqlc.arg(paper_account_id),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_hash),
    sqlc.arg(order_id)
)
ON CONFLICT (paper_account_id, idempotency_key) DO NOTHING
RETURNING order_id;

-- name: GetOrderRequest :one
SELECT *
FROM securities.order_requests
WHERE paper_account_id = sqlc.arg(paper_account_id)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: InsertOrderActionRequest :one
INSERT INTO securities.order_action_requests (
    paper_account_id,
    idempotency_key,
    request_hash,
    order_id,
    action
) VALUES (
    sqlc.arg(paper_account_id),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_hash),
    sqlc.arg(order_id),
    sqlc.arg(action)
)
ON CONFLICT (paper_account_id, idempotency_key) DO NOTHING
RETURNING order_id;

-- name: GetOrderActionRequest :one
SELECT *
FROM securities.order_action_requests
WHERE paper_account_id = sqlc.arg(paper_account_id)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: InsertOrder :one
INSERT INTO securities.orders (
    id,
    paper_account_id,
    instrument_id,
    symbol,
    client_order_reference,
    side,
    order_type,
    time_in_force,
    quantity,
    limit_price,
    status,
    reference_price,
    quote_status,
    quote_observed_at,
    replay_cursor,
    provider_order_id,
    policy_version
) VALUES (
    sqlc.arg(id),
    sqlc.arg(paper_account_id),
    sqlc.arg(instrument_id),
    sqlc.arg(symbol),
    sqlc.arg(client_order_reference),
    sqlc.arg(side),
    sqlc.arg(order_type),
    sqlc.arg(time_in_force),
    sqlc.arg(quantity),
    sqlc.narg(limit_price),
    'PENDING_SUBMISSION',
    sqlc.arg(reference_price),
    sqlc.arg(quote_status),
    sqlc.arg(quote_observed_at),
    sqlc.arg(replay_cursor),
    sqlc.arg(provider_order_id),
    sqlc.arg(policy_version)
)
RETURNING *;

-- name: GetOrder :one
SELECT *
FROM securities.orders
WHERE id = sqlc.arg(id);

-- name: GetOrderForUpdate :one
SELECT *
FROM securities.orders
WHERE id = sqlc.arg(id)
  AND paper_account_id = sqlc.arg(paper_account_id)
FOR UPDATE;

-- name: ListOrders :many
SELECT *
FROM securities.orders
WHERE paper_account_id = sqlc.arg(paper_account_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: MarkOrderOpen :one
UPDATE securities.orders
SET status = 'OPEN'
WHERE id = sqlc.arg(id)
  AND status = 'PENDING_SUBMISSION'
RETURNING *;

-- name: AdvanceOpenOrder :one
UPDATE securities.orders
SET replay_cursor = sqlc.arg(replay_cursor)
WHERE id = sqlc.arg(id)
  AND status IN ('OPEN', 'PARTIALLY_FILLED')
RETURNING *;

-- name: ApplyOrderFill :one
UPDATE securities.orders
SET status = sqlc.arg(status),
    filled_quantity = sqlc.arg(filled_quantity),
    average_fill_price = sqlc.arg(average_fill_price),
    replay_cursor = sqlc.arg(replay_cursor)
WHERE id = sqlc.arg(id)
  AND status IN ('PENDING_SUBMISSION', 'OPEN', 'PARTIALLY_FILLED')
RETURNING *;

-- name: MarkOrderRejected :one
UPDATE securities.orders
SET status = 'REJECTED',
    rejection_code = sqlc.arg(rejection_code),
    replay_cursor = sqlc.arg(replay_cursor)
WHERE id = sqlc.arg(id)
  AND status IN ('PENDING_SUBMISSION', 'OPEN')
RETURNING *;

-- name: MarkOrderExpired :one
UPDATE securities.orders
SET status = 'EXPIRED',
    replay_cursor = sqlc.arg(replay_cursor)
WHERE id = sqlc.arg(id)
  AND status IN ('OPEN', 'PARTIALLY_FILLED')
RETURNING *;

-- name: MarkOrderCancelled :one
UPDATE securities.orders
SET status = 'CANCELLED'
WHERE id = sqlc.arg(id)
  AND status IN ('OPEN', 'PARTIALLY_FILLED')
RETURNING *;

-- name: InsertBrokerEvent :one
INSERT INTO securities.broker_events (
    provider,
    external_event_id,
    order_id,
    payload_hash,
    event_type
) VALUES (
    sqlc.arg(provider),
    sqlc.arg(external_event_id),
    sqlc.arg(order_id),
    sqlc.arg(payload_hash),
    sqlc.arg(event_type)
)
ON CONFLICT (provider, external_event_id) DO NOTHING
RETURNING *;

-- name: GetBrokerEvent :one
SELECT *
FROM securities.broker_events
WHERE provider = sqlc.arg(provider)
  AND external_event_id = sqlc.arg(external_event_id);

-- name: InsertFill :one
INSERT INTO securities.fills (
    order_id,
    provider,
    external_event_id,
    fill_sequence,
    quantity,
    price,
    consideration,
    occurred_at
) VALUES (
    sqlc.arg(order_id),
    sqlc.arg(provider),
    sqlc.arg(external_event_id),
    sqlc.arg(fill_sequence),
    sqlc.arg(quantity),
    sqlc.arg(price),
    sqlc.arg(consideration),
    sqlc.arg(occurred_at)
)
RETURNING *;

-- name: ListFillsForOrder :many
SELECT *
FROM securities.fills
WHERE order_id = sqlc.arg(order_id)
ORDER BY fill_sequence;

-- name: GetPositionForUpdate :one
SELECT *
FROM securities.positions
WHERE paper_account_id = sqlc.arg(paper_account_id)
  AND instrument_id = sqlc.arg(instrument_id)
FOR UPDATE;

-- name: UpsertPosition :one
INSERT INTO securities.positions (
    paper_account_id,
    instrument_id,
    symbol,
    quantity,
    average_cost,
    cost_basis,
    realized_pnl
) VALUES (
    sqlc.arg(paper_account_id),
    sqlc.arg(instrument_id),
    sqlc.arg(symbol),
    sqlc.arg(quantity),
    sqlc.arg(average_cost),
    sqlc.arg(cost_basis),
    sqlc.arg(realized_pnl)
)
ON CONFLICT (paper_account_id, instrument_id) DO UPDATE
SET quantity = EXCLUDED.quantity,
    average_cost = EXCLUDED.average_cost,
    cost_basis = EXCLUDED.cost_basis,
    realized_pnl = EXCLUDED.realized_pnl,
    updated_at = clock_timestamp(),
    version = securities.positions.version + 1
RETURNING *;

-- name: ListPositions :many
SELECT *
FROM securities.positions
WHERE paper_account_id = sqlc.arg(paper_account_id)
  AND quantity > 0
ORDER BY symbol;

-- name: SumActivePositionReservations :one
SELECT COALESCE(sum(quantity), 0)::numeric AS quantity
FROM securities.position_reservations
WHERE paper_account_id = sqlc.arg(paper_account_id)
  AND instrument_id = sqlc.arg(instrument_id)
  AND status = 'ACTIVE';

-- name: InsertPositionReservation :one
INSERT INTO securities.position_reservations (
    id, paper_account_id, instrument_id, symbol, reservation_type, quantity,
    customer_ledger_account_id, locked_ledger_account_id, lock_ledger_transaction_id
) VALUES (
    sqlc.arg(id), sqlc.arg(paper_account_id), sqlc.arg(instrument_id), sqlc.arg(symbol),
    'RWA_LOCK', sqlc.arg(quantity), sqlc.arg(customer_ledger_account_id),
    sqlc.arg(locked_ledger_account_id), sqlc.arg(lock_ledger_transaction_id)
)
RETURNING *;

-- name: GetPositionReservation :one
SELECT * FROM securities.position_reservations WHERE id = sqlc.arg(id);

-- name: GetPositionReservationForUpdate :one
SELECT * FROM securities.position_reservations WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ReleasePositionReservation :one
UPDATE securities.position_reservations
SET status = 'RELEASED',
    release_ledger_transaction_id = sqlc.arg(release_ledger_transaction_id),
    released_at = clock_timestamp(),
    version = version + 1
WHERE id = sqlc.arg(id) AND status = 'ACTIVE'
RETURNING *;
