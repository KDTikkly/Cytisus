-- name: InsertOutboxEvent :one
INSERT INTO outbox.events (
    aggregate_type,
    aggregate_id,
    event_type,
    event_version,
    payload,
    max_attempts
) VALUES (
    sqlc.arg(aggregate_type),
    sqlc.arg(aggregate_id),
    sqlc.arg(event_type),
    sqlc.arg(event_version),
    sqlc.arg(payload),
    sqlc.arg(max_attempts)
)
RETURNING *;

-- name: ClaimOutboxEvents :many
WITH candidates AS (
    SELECT event.id
    FROM outbox.events AS event
    WHERE event.attempt_count < event.max_attempts
      AND (
          (event.status = 'PENDING' AND event.available_at <= clock_timestamp())
          OR (
              event.status = 'PROCESSING'
              AND event.claimed_at <= clock_timestamp() - make_interval(secs => sqlc.arg(lease_seconds)::INTEGER)
          )
      )
    ORDER BY event.available_at, event.created_at, event.id
    FOR UPDATE SKIP LOCKED
    LIMIT sqlc.arg(batch_size)
)
UPDATE outbox.events AS event
SET status = 'PROCESSING',
    attempt_count = event.attempt_count + 1,
    claimed_by = sqlc.arg(worker_id),
    claimed_at = clock_timestamp()
FROM candidates
WHERE event.id = candidates.id
RETURNING event.*;

-- name: MarkOutboxDelivered :one
UPDATE outbox.events
SET status = 'DELIVERED',
    delivered_at = clock_timestamp(),
    last_error = NULL
WHERE id = sqlc.arg(id)
  AND status = 'PROCESSING'
  AND claimed_by = sqlc.arg(worker_id)
RETURNING *;

-- name: MarkOutboxFailed :one
UPDATE outbox.events
SET status = CASE
        WHEN attempt_count >= max_attempts THEN 'DEAD_LETTER'
        ELSE 'PENDING'
    END,
    available_at = CASE
        WHEN attempt_count >= max_attempts THEN available_at
        ELSE clock_timestamp() + make_interval(
            secs => LEAST(
                sqlc.arg(max_backoff_seconds)::INTEGER,
                sqlc.arg(base_backoff_seconds)::INTEGER
                    * (1 << LEAST(GREATEST(attempt_count - 1, 0), 20))
            )
        )
    END,
    claimed_by = NULL,
    claimed_at = NULL,
    last_error = LEFT(sqlc.arg(error_message), 1024)
WHERE id = sqlc.arg(id)
  AND status = 'PROCESSING'
  AND claimed_by = sqlc.arg(worker_id)
RETURNING *;

-- name: ReplayDeadLetterEvent :one
UPDATE outbox.events
SET status = 'PENDING',
    attempt_count = 0,
    available_at = clock_timestamp(),
    claimed_by = NULL,
    claimed_at = NULL,
    delivered_at = NULL,
    last_error = NULL
WHERE id = sqlc.arg(id)
  AND status = 'DEAD_LETTER'
RETURNING *;

-- name: CancelOutboxEvent :one
UPDATE outbox.events
SET status = 'CANCELLED',
    cancelled_at = clock_timestamp(),
    claimed_by = NULL,
    claimed_at = NULL
WHERE id = sqlc.arg(id)
  AND status IN ('PENDING', 'DEAD_LETTER')
RETURNING *;

-- name: GetOutboxEvent :one
SELECT *
FROM outbox.events
WHERE id = sqlc.arg(id);

-- name: ListOutboxEventsByStatus :many
SELECT *
FROM outbox.events
WHERE status = sqlc.arg(status)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size)
OFFSET sqlc.arg(page_offset);

-- name: InsertConsumerReceipt :one
INSERT INTO outbox.consumer_receipts (
    consumer_name,
    event_id
) VALUES (
    sqlc.arg(consumer_name),
    sqlc.arg(event_id)
)
ON CONFLICT (consumer_name, event_id) DO NOTHING
RETURNING *;

-- name: GetConsumerReceipt :one
SELECT *
FROM outbox.consumer_receipts
WHERE consumer_name = sqlc.arg(consumer_name)
  AND event_id = sqlc.arg(event_id);

-- name: InsertOutboxAdminAudit :one
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
    'outbox.event',
    sqlc.arg(resource_id),
    'ADMIN',
    sqlc.arg(actor_id),
    sqlc.narg(correlation_id),
    sqlc.arg(metadata)
)
RETURNING *;
