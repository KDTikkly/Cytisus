-- name: DatabaseReady :one
SELECT 1::bigint AS ready;
