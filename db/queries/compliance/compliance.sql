-- name: CreateCase :one
INSERT INTO compliance.cases (
    id,
    customer_reference,
    case_type,
    resource_type,
    resource_id,
    status,
    reason_code,
    next_action,
    policy_version
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(case_type),
    sqlc.arg(resource_type),
    sqlc.arg(resource_id),
    sqlc.arg(status),
    sqlc.arg(reason_code),
    sqlc.arg(next_action),
    sqlc.arg(policy_version)
)
RETURNING *;

-- name: GetCase :one
SELECT *
FROM compliance.cases
WHERE id = sqlc.arg(id);

-- name: GetCaseForUpdate :one
SELECT *
FROM compliance.cases
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListCases :many
SELECT *
FROM compliance.cases
WHERE (sqlc.arg(status_filter)::TEXT = '' OR status = sqlc.arg(status_filter))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: TransitionCase :one
UPDATE compliance.cases
SET status = sqlc.arg(status),
    reason_code = sqlc.arg(reason_code),
    next_action = sqlc.arg(next_action)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertCaseEvent :one
INSERT INTO compliance.case_events (
    case_id,
    from_status,
    to_status,
    actor_type,
    actor_id,
    reason_code,
    metadata
) VALUES (
    sqlc.arg(case_id),
    sqlc.narg(from_status),
    sqlc.arg(to_status),
    sqlc.arg(actor_type),
    sqlc.arg(actor_id),
    sqlc.arg(reason_code),
    sqlc.arg(metadata)
)
RETURNING *;

-- name: CreateReviewProposal :one
INSERT INTO compliance.review_proposals (
    id,
    case_id,
    action,
    maker_id,
    maker_role,
    reason_code,
    ticket_reference,
    evidence_reference
) VALUES (
    sqlc.arg(id),
    sqlc.arg(case_id),
    sqlc.arg(action),
    sqlc.arg(maker_id),
    sqlc.arg(maker_role),
    sqlc.arg(reason_code),
    sqlc.arg(ticket_reference),
    sqlc.arg(evidence_reference)
)
RETURNING *;

-- name: GetReviewProposal :one
SELECT *
FROM compliance.review_proposals
WHERE id = sqlc.arg(id);

-- name: GetReviewProposalForUpdate :one
SELECT *
FROM compliance.review_proposals
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: DecideReviewProposal :one
UPDATE compliance.review_proposals
SET status = sqlc.arg(status),
    checker_id = sqlc.arg(checker_id),
    checker_role = sqlc.arg(checker_role),
    decision_reason = sqlc.arg(decision_reason),
    decided_at = clock_timestamp()
WHERE id = sqlc.arg(id)
  AND status = 'PENDING'
RETURNING *;

-- name: ListReviewProposalsForCase :many
SELECT *
FROM compliance.review_proposals
WHERE case_id = sqlc.arg(case_id)
ORDER BY created_at, id;
