-- name: CreateCustomerProfile :one
INSERT INTO banking.customer_profiles (
    customer_reference,
    paper_account_id,
    cash_ledger_account_id,
    provider_clearing_ledger_account_id,
    policy_version
) VALUES (
    sqlc.arg(customer_reference),
    sqlc.arg(paper_account_id),
    sqlc.arg(cash_ledger_account_id),
    sqlc.arg(provider_clearing_ledger_account_id),
    sqlc.arg(policy_version)
)
ON CONFLICT (customer_reference) DO NOTHING
RETURNING *;

-- name: GetCustomerProfile :one
SELECT *
FROM banking.customer_profiles
WHERE customer_reference = sqlc.arg(customer_reference);

-- name: GetCustomerProfileForUpdate :one
SELECT *
FROM banking.customer_profiles
WHERE customer_reference = sqlc.arg(customer_reference)
FOR UPDATE;

-- name: CreateBankAccount :one
INSERT INTO banking.bank_accounts (
    id,
    customer_reference,
    provider,
    external_account_reference,
    rail_support,
    owner_relation,
    ownership_status,
    status,
    risk_class,
    cooling_until,
    policy_version
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(provider),
    sqlc.arg(external_account_reference),
    sqlc.arg(rail_support),
    sqlc.arg(owner_relation),
    sqlc.arg(ownership_status),
    sqlc.arg(status),
    sqlc.arg(risk_class),
    sqlc.narg(cooling_until),
    sqlc.arg(policy_version)
)
ON CONFLICT (customer_reference, provider, external_account_reference) DO NOTHING
RETURNING *;

-- name: GetBankAccount :one
SELECT *
FROM banking.bank_accounts
WHERE id = sqlc.arg(id);

-- name: GetBankAccountForUpdate :one
SELECT *
FROM banking.bank_accounts
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: GetBankAccountByExternalReference :one
SELECT *
FROM banking.bank_accounts
WHERE customer_reference = sqlc.arg(customer_reference)
  AND provider = sqlc.arg(provider)
  AND external_account_reference = sqlc.arg(external_account_reference);

-- name: ListBankAccounts :many
SELECT *
FROM banking.bank_accounts
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY successfully_funded_at DESC NULLS LAST, created_at, id;

-- name: GetPreferredWithdrawalAccount :one
SELECT *
FROM banking.bank_accounts
WHERE customer_reference = sqlc.arg(customer_reference)
  AND owner_relation = 'SAME_NAME'
  AND ownership_status = 'VERIFIED'
  AND status = 'ACTIVE'
  AND successfully_funded_at IS NOT NULL
ORDER BY successfully_funded_at DESC, created_at
LIMIT 1;

-- name: UpdateBankAccountOwnership :one
UPDATE banking.bank_accounts
SET ownership_status = sqlc.arg(ownership_status),
    cooling_until = sqlc.narg(cooling_until)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkBankAccountFunded :one
UPDATE banking.bank_accounts
SET successfully_funded_at = COALESCE(successfully_funded_at, clock_timestamp())
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ClearBankAccountFunding :one
UPDATE banking.bank_accounts
SET successfully_funded_at = NULL
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertBankAccountEvent :one
INSERT INTO banking.bank_account_events (
    bank_account_id,
    event_type,
    actor_type,
    actor_id,
    metadata
) VALUES (
    sqlc.arg(bank_account_id),
    sqlc.arg(event_type),
    sqlc.arg(actor_type),
    sqlc.arg(actor_id),
    sqlc.arg(metadata)
)
RETURNING *;

-- name: InsertFundingRequest :one
INSERT INTO banking.funding_requests (
    customer_reference,
    idempotency_key,
    request_hash,
    transfer_id
) VALUES (
    sqlc.arg(customer_reference),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_hash),
    sqlc.arg(transfer_id)
)
ON CONFLICT (customer_reference, idempotency_key) DO NOTHING
RETURNING transfer_id;

-- name: GetFundingRequest :one
SELECT *
FROM banking.funding_requests
WHERE customer_reference = sqlc.arg(customer_reference)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateFundingTransfer :one
INSERT INTO banking.funding_transfers (
    id,
    customer_reference,
    bank_account_id,
    rail,
    amount,
    status,
    provider,
    provider_transfer_id,
    pending_ledger_transaction_id,
    reason_code,
    policy_version
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(bank_account_id),
    sqlc.arg(rail),
    sqlc.arg(amount),
    sqlc.arg(status),
    sqlc.arg(provider),
    sqlc.arg(provider_transfer_id),
    sqlc.narg(pending_ledger_transaction_id),
    sqlc.narg(reason_code),
    sqlc.arg(policy_version)
)
RETURNING *;

-- name: GetFundingTransfer :one
SELECT *
FROM banking.funding_transfers
WHERE id = sqlc.arg(id);

-- name: GetFundingTransferForUpdate :one
SELECT *
FROM banking.funding_transfers
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListFundingTransfers :many
SELECT *
FROM banking.funding_transfers
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: TransitionFundingTransfer :one
UPDATE banking.funding_transfers
SET status = sqlc.arg(status),
    reason_code = sqlc.narg(reason_code),
    pending_release_transaction_id = COALESCE(sqlc.narg(pending_release_transaction_id), pending_release_transaction_id),
    settled_ledger_transaction_id = COALESCE(sqlc.narg(settled_ledger_transaction_id), settled_ledger_transaction_id),
    return_reversal_transaction_id = COALESCE(sqlc.narg(return_reversal_transaction_id), return_reversal_transaction_id)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertWithdrawalRequest :one
INSERT INTO banking.withdrawal_requests (
    customer_reference,
    idempotency_key,
    request_hash,
    withdrawal_id
) VALUES (
    sqlc.arg(customer_reference),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_hash),
    sqlc.arg(withdrawal_id)
)
ON CONFLICT (customer_reference, idempotency_key) DO NOTHING
RETURNING withdrawal_id;

-- name: GetWithdrawalRequest :one
SELECT *
FROM banking.withdrawal_requests
WHERE customer_reference = sqlc.arg(customer_reference)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateWithdrawal :one
INSERT INTO banking.withdrawals (
    id,
    customer_reference,
    bank_account_id,
    amount,
    status,
    reason_code,
    next_action,
    compliance_case_id,
    cooling_until,
    reservation_ledger_transaction_id,
    provider,
    provider_transfer_id,
    policy_version
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(bank_account_id),
    sqlc.arg(amount),
    sqlc.arg(status),
    sqlc.arg(reason_code),
    sqlc.arg(next_action),
    sqlc.narg(compliance_case_id),
    sqlc.narg(cooling_until),
    sqlc.narg(reservation_ledger_transaction_id),
    sqlc.arg(provider),
    sqlc.arg(provider_transfer_id),
    sqlc.arg(policy_version)
)
RETURNING *;

-- name: GetWithdrawal :one
SELECT *
FROM banking.withdrawals
WHERE id = sqlc.arg(id);

-- name: GetWithdrawalForUpdate :one
SELECT *
FROM banking.withdrawals
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: GetWithdrawalByCaseForUpdate :one
SELECT *
FROM banking.withdrawals
WHERE compliance_case_id = sqlc.arg(compliance_case_id)
FOR UPDATE;

-- name: ListWithdrawals :many
SELECT *
FROM banking.withdrawals
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: TransitionWithdrawal :one
UPDATE banking.withdrawals
SET status = sqlc.arg(status),
    reason_code = sqlc.arg(reason_code),
    next_action = sqlc.arg(next_action),
    compliance_case_id = COALESCE(sqlc.narg(compliance_case_id), compliance_case_id),
    cooling_until = COALESCE(sqlc.narg(cooling_until), cooling_until),
    reservation_ledger_transaction_id = COALESCE(sqlc.narg(reservation_ledger_transaction_id), reservation_ledger_transaction_id),
    submitted_ledger_transaction_id = COALESCE(sqlc.narg(submitted_ledger_transaction_id), submitted_ledger_transaction_id),
    return_reservation_reversal_id = COALESCE(sqlc.narg(return_reservation_reversal_id), return_reservation_reversal_id),
    return_submitted_reversal_id = COALESCE(sqlc.narg(return_submitted_reversal_id), return_submitted_reversal_id)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertProviderEvent :one
INSERT INTO banking.provider_events (
    provider,
    external_event_id,
    resource_type,
    resource_id,
    event_type,
    payload_hash,
    ledger_transaction_id
) VALUES (
    sqlc.arg(provider),
    sqlc.arg(external_event_id),
    sqlc.arg(resource_type),
    sqlc.arg(resource_id),
    sqlc.arg(event_type),
    sqlc.arg(payload_hash),
    sqlc.narg(ledger_transaction_id)
)
ON CONFLICT (provider, external_event_id) DO NOTHING
RETURNING *;

-- name: GetProviderEvent :one
SELECT *
FROM banking.provider_events
WHERE provider = sqlc.arg(provider)
  AND external_event_id = sqlc.arg(external_event_id);

-- name: CreateReconciliationRun :one
INSERT INTO banking.reconciliation_runs (
    id,
    scope,
    policy_version
) VALUES (
    sqlc.arg(id),
    sqlc.arg(scope),
    sqlc.arg(policy_version)
)
RETURNING *;

-- name: InsertReconciliationItem :one
INSERT INTO banking.reconciliation_items (
    run_id,
    customer_reference,
    ledger_amount,
    provider_amount,
    difference,
    status,
    compliance_case_id
) VALUES (
    sqlc.arg(run_id),
    sqlc.arg(customer_reference),
    sqlc.arg(ledger_amount),
    sqlc.arg(provider_amount),
    sqlc.arg(difference),
    sqlc.arg(status),
    sqlc.narg(compliance_case_id)
)
RETURNING *;

-- name: CompleteReconciliationRun :one
UPDATE banking.reconciliation_runs
SET status = sqlc.arg(status),
    completed_at = clock_timestamp()
WHERE id = sqlc.arg(id)
  AND status = 'RUNNING'
RETURNING *;

-- name: ListReconciliationItems :many
SELECT *
FROM banking.reconciliation_items
WHERE run_id = sqlc.arg(run_id)
ORDER BY customer_reference;

-- name: SumProviderSettledFunding :one
SELECT COALESCE(SUM(
    CASE
        WHEN rail = 'ACH' AND status = 'SETTLED' THEN amount
        WHEN rail = 'WIRE' AND status = 'CREDITED' THEN amount
        ELSE 0
    END
), 0)::NUMERIC(38, 18) AS amount
FROM banking.funding_transfers
WHERE customer_reference = sqlc.arg(customer_reference);

-- name: SumProviderSettledWithdrawals :one
SELECT COALESCE(SUM(CASE WHEN status = 'SETTLED' THEN amount ELSE 0 END), 0)::NUMERIC(38, 18) AS amount
FROM banking.withdrawals
WHERE customer_reference = sqlc.arg(customer_reference);
