-- name: InsertEvent :one
INSERT INTO notification.events (
    id, notification_event_id, customer_reference, category, event_type,
    title_key, body_key, resource_type, resource_id, action_path
) VALUES (
    sqlc.arg(id), sqlc.arg(notification_event_id), sqlc.arg(customer_reference),
    sqlc.arg(category), sqlc.arg(event_type), sqlc.arg(title_key),
    sqlc.arg(body_key), sqlc.arg(resource_type), sqlc.arg(resource_id),
    sqlc.narg(action_path)
)
ON CONFLICT (notification_event_id) DO NOTHING
RETURNING *;

-- name: GetEventByKey :one
SELECT *
FROM notification.events
WHERE notification_event_id = sqlc.arg(notification_event_id);

-- name: InsertDelivery :one
INSERT INTO notification.deliveries (
    event_id, channel, provider, status, delivered_at
) VALUES (
    sqlc.arg(event_id), sqlc.arg(channel), sqlc.arg(provider),
    sqlc.arg(status), sqlc.narg(delivered_at)
)
ON CONFLICT (event_id, channel) DO NOTHING
RETURNING *;

-- name: ListInbox :many
SELECT
    events.*,
    deliveries.status AS delivery_status,
    deliveries.delivered_at
FROM notification.events AS events
JOIN notification.deliveries AS deliveries ON deliveries.event_id = events.id
WHERE events.customer_reference = sqlc.arg(customer_reference)
  AND deliveries.channel = 'IN_APP'
ORDER BY events.created_at DESC, events.id DESC
LIMIT sqlc.arg(page_size);

-- name: ListDeliveriesForEvent :many
SELECT *
FROM notification.deliveries
WHERE event_id = sqlc.arg(event_id)
ORDER BY channel;

-- name: ListDeliveryReconciliation :many
SELECT
    events.customer_reference,
    events.notification_event_id,
    deliveries.channel,
    deliveries.status,
    deliveries.failure_code,
    deliveries.delivered_at
FROM notification.deliveries AS deliveries
JOIN notification.events AS events ON events.id = deliveries.event_id
ORDER BY events.created_at DESC, deliveries.channel
LIMIT sqlc.arg(page_size);
