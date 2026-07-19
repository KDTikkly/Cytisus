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
    sqlc.narg(correlation_id),
    sqlc.arg(metadata)
)
RETURNING *;
