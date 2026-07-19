-- name: GetActivePolicy :one
SELECT * FROM rwa.policies
WHERE active AND effective_at <= clock_timestamp()
ORDER BY effective_at DESC LIMIT 1;

-- name: ListAssets :many
SELECT * FROM rwa.assets WHERE enabled ORDER BY underlying_symbol;

-- name: GetAsset :one
SELECT * FROM rwa.assets WHERE id = sqlc.arg(id) AND enabled;

-- name: GetAssetBySymbol :one
SELECT * FROM rwa.assets WHERE underlying_symbol = sqlc.arg(underlying_symbol) AND enabled;

-- name: GetCustomerProfile :one
SELECT * FROM rwa.customer_profiles WHERE customer_reference = sqlc.arg(customer_reference);

-- name: GetCustomerProfileForUpdate :one
SELECT * FROM rwa.customer_profiles WHERE customer_reference = sqlc.arg(customer_reference) FOR UPDATE;

-- name: CreateCustomerProfile :one
INSERT INTO rwa.customer_profiles (
    customer_reference, paper_account_id, cash_ledger_account_id, policy_version
) VALUES (
    sqlc.arg(customer_reference), sqlc.arg(paper_account_id),
    sqlc.arg(cash_ledger_account_id), sqlc.arg(policy_version)
)
ON CONFLICT (customer_reference) DO NOTHING
RETURNING *;

-- name: InsertCommandRequest :one
INSERT INTO rwa.command_requests (
    customer_reference, scope, idempotency_key, request_hash, resource_id
) VALUES (
    sqlc.arg(customer_reference), sqlc.arg(scope), sqlc.arg(idempotency_key),
    sqlc.arg(request_hash), sqlc.arg(resource_id)
)
ON CONFLICT (customer_reference, scope, idempotency_key) DO NOTHING
RETURNING *;

-- name: GetCommandRequest :one
SELECT * FROM rwa.command_requests
WHERE customer_reference = sqlc.arg(customer_reference)
  AND scope = sqlc.arg(scope)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateExternalAddress :one
INSERT INTO rwa.external_addresses (id, customer_reference, address)
VALUES (sqlc.arg(id), sqlc.arg(customer_reference), sqlc.arg(address))
RETURNING *;

-- name: GetExternalAddress :one
SELECT * FROM rwa.external_addresses WHERE id = sqlc.arg(id);

-- name: GetCustomerExternalAddressForUpdate :one
SELECT * FROM rwa.external_addresses
WHERE id = sqlc.arg(id) AND customer_reference = sqlc.arg(customer_reference)
FOR UPDATE;

-- name: ListExternalAddresses :many
SELECT * FROM rwa.external_addresses
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC;

-- name: MarkAddressProofVerified :one
UPDATE rwa.external_addresses
SET status = 'PROOF_VERIFIED', proof_reference = sqlc.arg(proof_reference),
    proof_verified_at = clock_timestamp(), updated_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id) AND status = 'PENDING_PROOF'
RETURNING *;

-- name: MarkAddressRiskReview :one
UPDATE rwa.external_addresses
SET status = 'RISK_REVIEW', updated_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id) AND status = 'PROOF_VERIFIED'
RETURNING *;

-- name: MarkAddressCooling :one
UPDATE rwa.external_addresses
SET status = 'COOLING', risk_reason_code = sqlc.arg(risk_reason_code),
    risk_reviewed_at = clock_timestamp(), cooling_ends_at = sqlc.arg(cooling_ends_at),
    updated_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id) AND status = 'RISK_REVIEW'
RETURNING *;

-- name: RejectExternalAddress :one
UPDATE rwa.external_addresses
SET status = 'REJECTED', risk_reason_code = sqlc.arg(risk_reason_code),
    risk_reviewed_at = clock_timestamp(), updated_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id) AND status = 'RISK_REVIEW'
RETURNING *;

-- name: ActivateExternalAddress :one
UPDATE rwa.external_addresses
SET status = 'ACTIVE', activated_at = clock_timestamp(), updated_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id) AND status = 'COOLING' AND cooling_ends_at <= clock_timestamp()
RETURNING *;

-- name: SuspendExternalAddress :one
UPDATE rwa.external_addresses
SET status = 'SUSPENDED', suspended_at = clock_timestamp(), activated_at = NULL,
    updated_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id) AND status = 'ACTIVE'
RETURNING *;

-- name: CreateUnderlyingLock :one
INSERT INTO rwa.underlying_locks (
    id, customer_reference, asset_id, reservation_id, quantity
) VALUES (
    sqlc.arg(id), sqlc.arg(customer_reference), sqlc.arg(asset_id),
    sqlc.arg(reservation_id), sqlc.arg(quantity)
)
RETURNING *;

-- name: GetUnderlyingLock :one
SELECT * FROM rwa.underlying_locks WHERE id = sqlc.arg(id);

-- name: GetUnderlyingLockForUpdate :one
SELECT * FROM rwa.underlying_locks WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ReleaseUnderlyingLock :one
UPDATE rwa.underlying_locks
SET status = 'RELEASED', released_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id) AND status = 'LOCKED'
RETURNING *;

-- name: SumLockedSharesByAsset :one
SELECT COALESCE(sum(quantity), 0)::numeric AS quantity
FROM rwa.underlying_locks
WHERE asset_id = sqlc.arg(asset_id) AND status = 'LOCKED';

-- name: CreateMintRequest :one
INSERT INTO rwa.mint_requests (
    id, customer_reference, asset_id, underlying_lock_id, quantity, custody_mode,
    destination_address_id, destination_address, operation_id
) VALUES (
    sqlc.arg(id), sqlc.arg(customer_reference), sqlc.arg(asset_id),
    sqlc.arg(underlying_lock_id), sqlc.arg(quantity), sqlc.arg(custody_mode),
    sqlc.narg(destination_address_id), sqlc.arg(destination_address), sqlc.arg(operation_id)
)
RETURNING *;

-- name: GetMintRequest :one
SELECT * FROM rwa.mint_requests WHERE id = sqlc.arg(id);

-- name: GetCustomerMintRequest :one
SELECT * FROM rwa.mint_requests
WHERE id = sqlc.arg(id) AND customer_reference = sqlc.arg(customer_reference);

-- name: GetMintRequestForUpdate :one
SELECT * FROM rwa.mint_requests WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: UpdateMintState :one
UPDATE rwa.mint_requests
SET status = sqlc.arg(status), chain_tx_hash = sqlc.narg(chain_tx_hash),
    failure_code = sqlc.narg(failure_code), updated_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListMintRequests :many
SELECT * FROM rwa.mint_requests
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC;

-- name: UpsertBeneficialHolding :one
INSERT INTO rwa.beneficial_holdings (
    customer_reference, asset_id, custody_mode, destination_address, quantity
) VALUES (
    sqlc.arg(customer_reference), sqlc.arg(asset_id), sqlc.arg(custody_mode),
    sqlc.arg(destination_address), sqlc.arg(quantity)
)
ON CONFLICT (customer_reference, asset_id) DO UPDATE
SET quantity = rwa.beneficial_holdings.quantity + EXCLUDED.quantity,
    custody_mode = EXCLUDED.custody_mode,
    destination_address = EXCLUDED.destination_address,
    updated_at = clock_timestamp(), version = rwa.beneficial_holdings.version + 1
RETURNING *;

-- name: GetBeneficialHolding :one
SELECT * FROM rwa.beneficial_holdings
WHERE customer_reference = sqlc.arg(customer_reference) AND asset_id = sqlc.arg(asset_id);

-- name: GetBeneficialHoldingForUpdate :one
SELECT * FROM rwa.beneficial_holdings
WHERE customer_reference = sqlc.arg(customer_reference) AND asset_id = sqlc.arg(asset_id)
FOR UPDATE;

-- name: ReduceBeneficialHolding :one
UPDATE rwa.beneficial_holdings
SET quantity = quantity - sqlc.arg(quantity), updated_at = clock_timestamp(), version = version + 1
WHERE customer_reference = sqlc.arg(customer_reference)
  AND asset_id = sqlc.arg(asset_id)
  AND quantity >= sqlc.arg(quantity)
RETURNING *;

-- name: ListBeneficialHoldings :many
SELECT * FROM rwa.beneficial_holdings
WHERE customer_reference = sqlc.arg(customer_reference) AND quantity > 0
ORDER BY asset_id;

-- name: ListPositiveHoldingsForAsset :many
SELECT * FROM rwa.beneficial_holdings
WHERE asset_id = sqlc.arg(asset_id) AND quantity > 0
ORDER BY customer_reference;

-- name: CreateRedemptionRequest :one
INSERT INTO rwa.redemption_requests (
    id, customer_reference, asset_id, underlying_lock_id, quantity,
    source_address, operation_id, forced
) VALUES (
    sqlc.arg(id), sqlc.arg(customer_reference), sqlc.arg(asset_id),
    sqlc.arg(underlying_lock_id), sqlc.arg(quantity), sqlc.arg(source_address),
    sqlc.arg(operation_id), sqlc.arg(forced)
)
RETURNING *;

-- name: GetRedemptionRequest :one
SELECT * FROM rwa.redemption_requests WHERE id = sqlc.arg(id);

-- name: GetCustomerRedemptionRequest :one
SELECT * FROM rwa.redemption_requests
WHERE id = sqlc.arg(id) AND customer_reference = sqlc.arg(customer_reference);

-- name: GetRedemptionRequestForUpdate :one
SELECT * FROM rwa.redemption_requests WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: UpdateRedemptionState :one
UPDATE rwa.redemption_requests
SET status = sqlc.arg(status), chain_tx_hash = sqlc.narg(chain_tx_hash),
    failure_code = sqlc.narg(failure_code),
    completed_at = CASE WHEN sqlc.arg(status)::text = 'COMPLETED' THEN clock_timestamp() ELSE NULL END,
    updated_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListRedemptionRequests :many
SELECT * FROM rwa.redemption_requests
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC;

-- name: CreateChainOperation :one
INSERT INTO rwa.chain_operations (
    operation_id, resource_type, resource_id, operation_type, max_attempts
) VALUES (
    sqlc.arg(operation_id), sqlc.arg(resource_type), sqlc.arg(resource_id),
    sqlc.arg(operation_type), sqlc.arg(max_attempts)
)
RETURNING *;

-- name: GetChainOperation :one
SELECT * FROM rwa.chain_operations WHERE operation_id = sqlc.arg(operation_id);

-- name: GetChainOperationForUpdate :one
SELECT * FROM rwa.chain_operations WHERE operation_id = sqlc.arg(operation_id) FOR UPDATE;

-- name: UpdateChainOperation :one
UPDATE rwa.chain_operations
SET status = sqlc.arg(status), tx_hash = sqlc.narg(tx_hash),
    attempt_count = attempt_count + 1, last_error_code = sqlc.narg(last_error_code),
    next_attempt_at = sqlc.arg(next_attempt_at), claimed_at = NULL, claimed_by = NULL,
    updated_at = clock_timestamp()
WHERE operation_id = sqlc.arg(operation_id)
RETURNING *;

-- name: ClaimChainOperations :many
WITH candidates AS (
    SELECT operation_id FROM rwa.chain_operations
    WHERE status IN ('PENDING', 'UNKNOWN')
      AND next_attempt_at <= clock_timestamp()
      AND attempt_count < max_attempts
      AND claimed_at IS NULL
    ORDER BY next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT sqlc.arg(batch_size)
)
UPDATE rwa.chain_operations AS operation
SET claimed_at = clock_timestamp(), claimed_by = sqlc.arg(worker_id), updated_at = clock_timestamp()
FROM candidates
WHERE operation.operation_id = candidates.operation_id
RETURNING operation.*;

-- name: MoveExhaustedOperationsToDLQ :execrows
UPDATE rwa.chain_operations
SET status = 'DLQ', claimed_at = NULL, claimed_by = NULL, updated_at = clock_timestamp()
WHERE status IN ('PENDING', 'UNKNOWN', 'FAILED') AND attempt_count >= max_attempts;

-- name: InsertProviderEvent :one
INSERT INTO rwa.provider_events (
    provider, external_event_id, event_type, payload_hash, operation_id
) VALUES (
    sqlc.arg(provider), sqlc.arg(external_event_id), sqlc.arg(event_type),
    sqlc.arg(payload_hash), sqlc.arg(operation_id)
)
ON CONFLICT (provider, external_event_id) DO NOTHING
RETURNING *;

-- name: GetProviderEvent :one
SELECT * FROM rwa.provider_events
WHERE provider = sqlc.arg(provider) AND external_event_id = sqlc.arg(external_event_id);

-- name: InsertStateEvent :one
INSERT INTO rwa.state_events (
    resource_type, resource_id, from_status, to_status, actor_type, actor_id, reason_code, metadata
) VALUES (
    sqlc.arg(resource_type), sqlc.arg(resource_id), sqlc.narg(from_status),
    sqlc.arg(to_status), sqlc.arg(actor_type), sqlc.arg(actor_id),
    sqlc.narg(reason_code), sqlc.arg(metadata)
)
RETURNING *;

-- name: InsertReconciliationRun :one
INSERT INTO rwa.reconciliation_runs (
    id, asset_id, chain_supply, locked_shares, difference, status,
    compliance_case_id, observed_block, daily_key
) VALUES (
    sqlc.arg(id), sqlc.arg(asset_id), sqlc.arg(chain_supply), sqlc.arg(locked_shares),
    sqlc.arg(difference), sqlc.arg(status), sqlc.narg(compliance_case_id), sqlc.arg(observed_block),
    sqlc.narg(daily_key)
)
RETURNING *;

-- name: ListReconciliationRuns :many
SELECT * FROM rwa.reconciliation_runs
WHERE asset_id = sqlc.arg(asset_id)
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: CreateDividend :one
INSERT INTO rwa.dividends (
    id, asset_id, external_reference, usd_per_share, record_at, payable_at, policy_version
) VALUES (
    sqlc.arg(id), sqlc.arg(asset_id), sqlc.arg(external_reference),
    sqlc.arg(usd_per_share), sqlc.arg(record_at), sqlc.arg(payable_at), sqlc.arg(policy_version)
)
RETURNING *;

-- name: GetDividend :one
SELECT * FROM rwa.dividends WHERE id = sqlc.arg(id);

-- name: GetDividendForUpdate :one
SELECT * FROM rwa.dividends WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: UpdateDividendStatus :one
UPDATE rwa.dividends
SET status = sqlc.arg(status), updated_at = clock_timestamp(), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertDividendEntitlement :one
INSERT INTO rwa.dividend_entitlements (
    id, dividend_id, customer_reference, whole_shares, gross_usd, withholding_usd, net_usd
) VALUES (
    sqlc.arg(id), sqlc.arg(dividend_id), sqlc.arg(customer_reference), sqlc.arg(whole_shares),
    sqlc.arg(gross_usd), sqlc.arg(withholding_usd), sqlc.arg(net_usd)
)
ON CONFLICT (dividend_id, customer_reference) DO NOTHING
RETURNING *;

-- name: ListDividendEntitlements :many
SELECT * FROM rwa.dividend_entitlements
WHERE dividend_id = sqlc.arg(dividend_id)
ORDER BY customer_reference;

-- name: GetDividendEntitlementForUpdate :one
SELECT * FROM rwa.dividend_entitlements WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: MarkDividendEntitlementCredited :one
UPDATE rwa.dividend_entitlements
SET status = 'USD_CASH_CREDITED', cash_ledger_transaction_id = sqlc.arg(cash_ledger_transaction_id),
    updated_at = clock_timestamp()
WHERE id = sqlc.arg(id) AND status = 'CALCULATED'
RETURNING *;
