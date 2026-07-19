-- name: GetActivePolicy :one
SELECT *
FROM card.policies
WHERE active
  AND effective_at <= clock_timestamp()
ORDER BY effective_at DESC
LIMIT 1;

-- name: GetPolicy :one
SELECT *
FROM card.policies
WHERE policy_version = sqlc.arg(policy_version);

-- name: GetCustomerProfile :one
SELECT *
FROM card.customer_profiles
WHERE customer_reference = sqlc.arg(customer_reference);

-- name: GetCustomerProfileForUpdate :one
SELECT *
FROM card.customer_profiles
WHERE customer_reference = sqlc.arg(customer_reference)
FOR UPDATE;

-- name: CreateCustomerProfile :one
INSERT INTO card.customer_profiles (
    customer_reference,
    paper_account_id,
    cash_ledger_account_id,
    receivable_ledger_account_id,
    provider_clearing_ledger_account_id,
    policy_version
) VALUES (
    sqlc.arg(customer_reference),
    sqlc.arg(paper_account_id),
    sqlc.arg(cash_ledger_account_id),
    sqlc.arg(receivable_ledger_account_id),
    sqlc.arg(provider_clearing_ledger_account_id),
    sqlc.arg(policy_version)
)
ON CONFLICT (customer_reference) DO NOTHING
RETURNING *;

-- name: UpdateRepaymentMode :one
UPDATE card.customer_profiles
SET repayment_mode = sqlc.arg(repayment_mode),
    updated_at = clock_timestamp(),
    version = version + 1
WHERE customer_reference = sqlc.arg(customer_reference)
RETURNING *;

-- name: FreezeSpending :one
UPDATE card.customer_profiles
SET spending_status = 'FROZEN',
    spending_status_reason = sqlc.arg(reason_code),
    updated_at = clock_timestamp(),
    version = version + 1
WHERE customer_reference = sqlc.arg(customer_reference)
RETURNING *;

-- name: UnfreezeSpending :one
UPDATE card.customer_profiles
SET spending_status = 'ACTIVE',
    spending_status_reason = NULL,
    updated_at = clock_timestamp(),
    version = version + 1
WHERE customer_reference = sqlc.arg(customer_reference)
RETURNING *;

-- name: InsertCommandRequest :one
INSERT INTO card.command_requests (
    customer_reference, scope, idempotency_key, request_hash, resource_id
) VALUES (
    sqlc.arg(customer_reference), sqlc.arg(scope), sqlc.arg(idempotency_key),
    sqlc.arg(request_hash), sqlc.arg(resource_id)
)
ON CONFLICT (customer_reference, scope, idempotency_key) DO NOTHING
RETURNING resource_id;

-- name: GetCommandRequest :one
SELECT *
FROM card.command_requests
WHERE customer_reference = sqlc.arg(customer_reference)
  AND scope = sqlc.arg(scope)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: BindCommandRequestResource :exec
UPDATE card.command_requests
SET resource_id = sqlc.arg(resource_id)
WHERE customer_reference = sqlc.arg(customer_reference)
  AND scope = sqlc.arg(scope)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateCard :one
INSERT INTO card.cards (
    id, customer_reference, card_type, status, display_name, last4,
    provider, provider_card_reference, apple_wallet_status,
    google_wallet_status, replacement_for_card_id, policy_version
) VALUES (
    sqlc.arg(id), sqlc.arg(customer_reference), sqlc.arg(card_type), sqlc.arg(status),
    sqlc.arg(display_name), sqlc.arg(last4), sqlc.arg(provider),
    sqlc.arg(provider_card_reference), sqlc.arg(apple_wallet_status),
    sqlc.arg(google_wallet_status), sqlc.narg(replacement_for_card_id),
    sqlc.arg(policy_version)
)
RETURNING *;

-- name: GetCard :one
SELECT *
FROM card.cards
WHERE id = sqlc.arg(id);

-- name: GetCustomerCard :one
SELECT *
FROM card.cards
WHERE id = sqlc.arg(id)
  AND customer_reference = sqlc.arg(customer_reference);

-- name: GetCustomerCardForUpdate :one
SELECT *
FROM card.cards
WHERE id = sqlc.arg(id)
  AND customer_reference = sqlc.arg(customer_reference)
FOR UPDATE;

-- name: GetCardForUpdate :one
SELECT *
FROM card.cards
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListCards :many
SELECT *
FROM card.cards
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC;

-- name: UpdateCardState :one
UPDATE card.cards
SET status = sqlc.arg(status),
    pin_set = sqlc.arg(pin_set),
    updated_at = clock_timestamp(),
    version = version + 1
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertLifecycleEvent :one
INSERT INTO card.lifecycle_events (
    card_id, from_status, to_status, action, actor_type, actor_id, reason_code
) VALUES (
    sqlc.arg(card_id), sqlc.narg(from_status), sqlc.arg(to_status), sqlc.arg(action),
    sqlc.arg(actor_type), sqlc.arg(actor_id), sqlc.narg(reason_code)
)
RETURNING *;

-- name: ListLifecycleEvents :many
SELECT *
FROM card.lifecycle_events
WHERE card_id = sqlc.arg(card_id)
ORDER BY occurred_at, id;

-- name: ListCollateralAssets :many
SELECT *
FROM card.collateral_assets
WHERE enabled
ORDER BY symbol;

-- name: GetCollateralAsset :one
SELECT *
FROM card.collateral_assets
WHERE symbol = sqlc.arg(symbol)
  AND enabled;

-- name: UpdateCollateralQuote :one
UPDATE card.collateral_assets
SET reference_price_usd = sqlc.arg(reference_price_usd),
    quote_status = sqlc.arg(quote_status),
    market_status = sqlc.arg(market_status),
    observed_at = sqlc.arg(observed_at)
WHERE symbol = sqlc.arg(symbol)
RETURNING *;

-- name: GetCollateralAccount :one
SELECT *
FROM card.collateral_accounts
WHERE customer_reference = sqlc.arg(customer_reference)
  AND symbol = sqlc.arg(symbol);

-- name: InsertCollateralAccount :one
INSERT INTO card.collateral_accounts (
    customer_reference, symbol, customer_ledger_account_id,
    provider_inventory_ledger_account_id
) VALUES (
    sqlc.arg(customer_reference), sqlc.arg(symbol), sqlc.arg(customer_ledger_account_id),
    sqlc.arg(provider_inventory_ledger_account_id)
)
ON CONFLICT (customer_reference, symbol) DO NOTHING
RETURNING *;

-- name: ListCollateralAccounts :many
SELECT
    accounts.customer_reference,
    accounts.symbol,
    accounts.source_type,
    accounts.customer_ledger_account_id,
    accounts.provider_inventory_ledger_account_id,
    accounts.frozen,
    accounts.transferring,
    assets.display_name,
    assets.asset_class,
    assets.haircut_rate,
    assets.reference_price_usd,
    assets.quote_status,
    assets.market_status,
    assets.observed_at,
    assets.policy_version
FROM card.collateral_accounts AS accounts
JOIN card.collateral_assets AS assets ON assets.symbol = accounts.symbol
WHERE accounts.customer_reference = sqlc.arg(customer_reference)
ORDER BY accounts.symbol;

-- name: UpsertAutoSellMandate :one
INSERT INTO card.auto_sell_mandates (
    id, customer_reference, enabled, allow_fractional, daily_max_usd,
    valid_until, policy_version
) VALUES (
    sqlc.arg(id), sqlc.arg(customer_reference), sqlc.arg(enabled),
    sqlc.arg(allow_fractional), sqlc.arg(daily_max_usd), sqlc.arg(valid_until),
    sqlc.arg(policy_version)
)
ON CONFLICT (customer_reference) DO UPDATE
SET enabled = EXCLUDED.enabled,
    allow_fractional = EXCLUDED.allow_fractional,
    daily_max_usd = EXCLUDED.daily_max_usd,
    valid_until = EXCLUDED.valid_until,
    policy_version = EXCLUDED.policy_version,
    updated_at = clock_timestamp(),
    version = card.auto_sell_mandates.version + 1
RETURNING *;

-- name: GetAutoSellMandate :one
SELECT *
FROM card.auto_sell_mandates
WHERE customer_reference = sqlc.arg(customer_reference);

-- name: GetAutoSellMandateForUpdate :one
SELECT *
FROM card.auto_sell_mandates
WHERE customer_reference = sqlc.arg(customer_reference)
FOR UPDATE;

-- name: DeleteAutoSellMandateAssets :exec
DELETE FROM card.auto_sell_mandate_assets
WHERE mandate_id = sqlc.arg(mandate_id);

-- name: InsertAutoSellMandateAsset :one
INSERT INTO card.auto_sell_mandate_assets (
    mandate_id, priority, symbol, minimum_retain_quantity
) VALUES (
    sqlc.arg(mandate_id), sqlc.arg(priority), sqlc.arg(symbol),
    sqlc.arg(minimum_retain_quantity)
)
RETURNING *;

-- name: ListAutoSellMandateAssets :many
SELECT *
FROM card.auto_sell_mandate_assets
WHERE mandate_id = sqlc.arg(mandate_id)
ORDER BY priority;

-- name: InsertProviderEvent :one
INSERT INTO card.provider_events (
    provider, external_event_id, event_type, payload_hash, resource_type, resource_id
) VALUES (
    sqlc.arg(provider), sqlc.arg(external_event_id), sqlc.arg(event_type),
    sqlc.arg(payload_hash), sqlc.arg(resource_type), sqlc.arg(resource_id)
)
ON CONFLICT (provider, external_event_id) DO NOTHING
RETURNING resource_id;

-- name: GetProviderEvent :one
SELECT *
FROM card.provider_events
WHERE provider = sqlc.arg(provider)
  AND external_event_id = sqlc.arg(external_event_id);

-- name: CreateAuthorization :one
INSERT INTO card.authorizations (
    id, customer_reference, card_id, provider, external_authorization_id,
    merchant_name, merchant_category_code, merchant_amount, merchant_currency,
    authorization_fx_rate, fx_markup_rate, authorized_usd, status, offline,
    entry_mode, occurred_at, decline_code, hold_ledger_transaction_id, policy_version
) VALUES (
    sqlc.arg(id), sqlc.arg(customer_reference), sqlc.arg(card_id), sqlc.arg(provider),
    sqlc.arg(external_authorization_id), sqlc.arg(merchant_name),
    sqlc.arg(merchant_category_code), sqlc.arg(merchant_amount),
    sqlc.arg(merchant_currency), sqlc.arg(authorization_fx_rate),
    sqlc.arg(fx_markup_rate), sqlc.arg(authorized_usd), sqlc.arg(status),
    sqlc.arg(offline), sqlc.arg(entry_mode), sqlc.arg(occurred_at), sqlc.narg(decline_code),
    sqlc.narg(hold_ledger_transaction_id), sqlc.arg(policy_version)
)
RETURNING *;

-- name: GetAuthorization :one
SELECT *
FROM card.authorizations
WHERE id = sqlc.arg(id);

-- name: GetCustomerAuthorization :one
SELECT *
FROM card.authorizations
WHERE id = sqlc.arg(id)
  AND customer_reference = sqlc.arg(customer_reference);

-- name: GetAuthorizationForUpdate :one
SELECT *
FROM card.authorizations
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListAuthorizations :many
SELECT *
FROM card.authorizations
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateAuthorizationTotals :one
UPDATE card.authorizations
SET status = sqlc.arg(status),
    captured_usd = sqlc.arg(captured_usd),
    reversed_usd = sqlc.arg(reversed_usd),
    updated_at = clock_timestamp(),
    version = version + 1
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateCapture :one
INSERT INTO card.captures (
    id, authorization_id, provider, external_capture_id, merchant_amount,
    merchant_currency, clearing_fx_rate, fx_markup_rate, settled_usd,
    tip_usd, hold_released_usd, cash_repaid_usd, auto_sell_repaid_usd,
    receivable_ledger_transaction_id, repayment_ledger_transaction_id, occurred_at
) VALUES (
    sqlc.arg(id), sqlc.arg(authorization_id), sqlc.arg(provider),
    sqlc.arg(external_capture_id), sqlc.arg(merchant_amount),
    sqlc.arg(merchant_currency), sqlc.arg(clearing_fx_rate),
    sqlc.arg(fx_markup_rate), sqlc.arg(settled_usd), sqlc.arg(tip_usd),
    sqlc.arg(hold_released_usd), sqlc.arg(cash_repaid_usd),
    sqlc.arg(auto_sell_repaid_usd), sqlc.arg(receivable_ledger_transaction_id),
    sqlc.narg(repayment_ledger_transaction_id), sqlc.arg(occurred_at)
)
RETURNING *;

-- name: GetCapture :one
SELECT *
FROM card.captures
WHERE id = sqlc.arg(id);

-- name: GetCaptureForUpdate :one
SELECT *
FROM card.captures
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListCapturesForAuthorization :many
SELECT *
FROM card.captures
WHERE authorization_id = sqlc.arg(authorization_id)
ORDER BY created_at, id;

-- name: ListCustomerCaptures :many
SELECT captures.*
FROM card.captures AS captures
JOIN card.authorizations AS authorizations ON authorizations.id = captures.authorization_id
WHERE authorizations.customer_reference = sqlc.arg(customer_reference)
ORDER BY captures.created_at DESC, captures.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateCaptureRefund :one
UPDATE card.captures
SET refunded_usd = sqlc.arg(refunded_usd),
    status = sqlc.arg(status)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkCaptureDisputed :one
UPDATE card.captures
SET status = 'DISPUTED'
WHERE id = sqlc.arg(id)
  AND status IN ('CAPTURED', 'PARTIALLY_REFUNDED')
RETURNING *;

-- name: CreateReversal :one
INSERT INTO card.reversals (
    id, authorization_id, provider, external_reversal_id,
    reversed_usd, ledger_transaction_id, occurred_at
) VALUES (
    sqlc.arg(id), sqlc.arg(authorization_id), sqlc.arg(provider),
    sqlc.arg(external_reversal_id), sqlc.arg(reversed_usd),
    sqlc.arg(ledger_transaction_id), sqlc.arg(occurred_at)
)
RETURNING *;

-- name: CreateRefund :one
INSERT INTO card.refunds (
    id, capture_id, provider, external_refund_id, refund_usd,
    receivable_reduction_usd, cash_credit_usd, ledger_transaction_id, occurred_at
) VALUES (
    sqlc.arg(id), sqlc.arg(capture_id), sqlc.arg(provider),
    sqlc.arg(external_refund_id), sqlc.arg(refund_usd),
    sqlc.arg(receivable_reduction_usd), sqlc.arg(cash_credit_usd),
    sqlc.arg(ledger_transaction_id), sqlc.arg(occurred_at)
)
RETURNING *;

-- name: GetRefund :one
SELECT *
FROM card.refunds
WHERE id = sqlc.arg(id);

-- name: ListRefundsForCapture :many
SELECT *
FROM card.refunds
WHERE capture_id = sqlc.arg(capture_id)
ORDER BY created_at, id;

-- name: CreateDispute :one
INSERT INTO card.disputes (
    id, capture_id, customer_reference, amount_usd, reason_code,
    compliance_case_id
) VALUES (
    sqlc.arg(id), sqlc.arg(capture_id), sqlc.arg(customer_reference),
    sqlc.arg(amount_usd), sqlc.arg(reason_code), sqlc.arg(compliance_case_id)
)
RETURNING *;

-- name: GetDispute :one
SELECT *
FROM card.disputes
WHERE id = sqlc.arg(id);

-- name: GetDisputeForUpdate :one
SELECT *
FROM card.disputes
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListDisputes :many
SELECT *
FROM card.disputes
WHERE (sqlc.arg(customer_filter)::TEXT = '' OR customer_reference = sqlc.arg(customer_filter))
ORDER BY opened_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: SumDisputedAmountForCapture :one
SELECT COALESCE(SUM(amount_usd), 0)::NUMERIC(38, 18) AS total_usd
FROM card.disputes
WHERE capture_id = sqlc.arg(capture_id)
  AND status IN ('OPEN', 'UNDER_REVIEW', 'RESOLVED')
  AND (status <> 'RESOLVED' OR outcome = 'ACCEPTED');

-- name: SumAcceptedDisputeAmountForCapture :one
SELECT COALESCE(SUM(amount_usd), 0)::NUMERIC(38, 18) AS total_usd
FROM card.disputes
WHERE capture_id = sqlc.arg(capture_id)
  AND status = 'RESOLVED'
  AND outcome = 'ACCEPTED';

-- name: ResolveDispute :one
UPDATE card.disputes
SET status = 'RESOLVED',
    outcome = sqlc.arg(outcome),
    resolution_ledger_transaction_id = sqlc.narg(resolution_ledger_transaction_id),
    resolved_at = clock_timestamp(),
    version = version + 1
WHERE id = sqlc.arg(id)
  AND status IN ('OPEN', 'UNDER_REVIEW')
RETURNING *;

-- name: InsertAutoSellExecution :one
INSERT INTO card.auto_sell_executions (
    id, capture_id, mandate_id, execution_sequence, symbol,
    requested_quantity, protected_limit_price, execution_price,
    filled_quantity, proceeds_usd, status, failure_code,
    ledger_transaction_id
) VALUES (
    sqlc.arg(id), sqlc.arg(capture_id), sqlc.arg(mandate_id),
    sqlc.arg(execution_sequence), sqlc.arg(symbol), sqlc.arg(requested_quantity),
    sqlc.arg(protected_limit_price), sqlc.arg(execution_price),
    sqlc.arg(filled_quantity), sqlc.arg(proceeds_usd), sqlc.arg(status),
    sqlc.narg(failure_code), sqlc.narg(ledger_transaction_id)
)
RETURNING *;

-- name: ListAutoSellExecutions :many
SELECT *
FROM card.auto_sell_executions
WHERE capture_id = sqlc.arg(capture_id)
ORDER BY execution_sequence;

-- name: SumDailyAutoSell :one
SELECT COALESCE(SUM(executions.proceeds_usd), 0)::NUMERIC(38, 18) AS total_usd
FROM card.auto_sell_executions AS executions
JOIN card.captures AS captures ON captures.id = executions.capture_id
JOIN card.authorizations AS authorizations ON authorizations.id = captures.authorization_id
WHERE authorizations.customer_reference = sqlc.arg(customer_reference)
  AND executions.created_at >= sqlc.arg(since_time)
  AND executions.status IN ('FILLED', 'PARTIALLY_FILLED');

-- name: CreateStatement :one
INSERT INTO card.statements (
    id, customer_reference, period_start, period_end, amount_due_usd, due_at
) VALUES (
    sqlc.arg(id), sqlc.arg(customer_reference), sqlc.arg(period_start),
    sqlc.arg(period_end), sqlc.arg(amount_due_usd), sqlc.arg(due_at)
)
ON CONFLICT (customer_reference, period_start, period_end) DO UPDATE
SET amount_due_usd = EXCLUDED.amount_due_usd,
    due_at = EXCLUDED.due_at
WHERE card.statements.status = 'OPEN'
RETURNING *;

-- name: ListStatements :many
SELECT *
FROM card.statements
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY period_end DESC, id DESC;

-- name: GetStatementForUpdate :one
SELECT *
FROM card.statements
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: GetStatement :one
SELECT *
FROM card.statements
WHERE id = sqlc.arg(id);

-- name: GetStatementByPeriod :one
SELECT *
FROM card.statements
WHERE customer_reference = sqlc.arg(customer_reference)
  AND period_start = sqlc.arg(period_start)
  AND period_end = sqlc.arg(period_end);

-- name: MarkStatementPaid :one
UPDATE card.statements
SET status = 'PAID',
    paid_at = clock_timestamp(),
    repayment_ledger_transaction_id = sqlc.arg(repayment_ledger_transaction_id)
WHERE id = sqlc.arg(id)
  AND status IN ('OPEN', 'PAST_DUE')
RETURNING *;

-- name: InsertReconciliationRun :one
INSERT INTO card.reconciliation_runs (
    id, customer_reference, ledger_hold_usd, provider_hold_usd,
    ledger_receivable_usd, provider_receivable_usd, difference_usd,
    status, compliance_case_id, started_at, completed_at
) VALUES (
    sqlc.arg(id), sqlc.arg(customer_reference), sqlc.arg(ledger_hold_usd),
    sqlc.arg(provider_hold_usd), sqlc.arg(ledger_receivable_usd),
    sqlc.arg(provider_receivable_usd), sqlc.arg(difference_usd),
    sqlc.arg(status), sqlc.narg(compliance_case_id), sqlc.arg(started_at),
    sqlc.arg(completed_at)
)
RETURNING *;

-- name: ListReconciliationRuns :many
SELECT *
FROM card.reconciliation_runs
WHERE (sqlc.arg(customer_filter)::TEXT = '' OR customer_reference = sqlc.arg(customer_filter))
ORDER BY completed_at DESC, id DESC
LIMIT sqlc.arg(page_size);
