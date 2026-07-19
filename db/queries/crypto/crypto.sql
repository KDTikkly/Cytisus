-- name: ListAssets :many
SELECT *
FROM crypto.assets
WHERE tradable
ORDER BY symbol;

-- name: GetAsset :one
SELECT *
FROM crypto.assets
WHERE symbol = sqlc.arg(symbol)
  AND tradable;

-- name: ListAssetNetworks :many
SELECT
    asset_networks.asset_symbol,
    asset_networks.network_code,
    networks.display_name,
    asset_networks.deposit_enabled,
    asset_networks.withdrawal_enabled,
    asset_networks.native_deployment
FROM crypto.asset_networks AS asset_networks
JOIN crypto.networks AS networks
    ON networks.network_code = asset_networks.network_code
WHERE asset_networks.asset_symbol = sqlc.arg(asset_symbol)
  AND networks.active
ORDER BY asset_networks.network_code;

-- name: GetAssetNetwork :one
SELECT asset_networks.*
FROM crypto.asset_networks AS asset_networks
JOIN crypto.networks AS networks
    ON networks.network_code = asset_networks.network_code
WHERE asset_networks.asset_symbol = sqlc.arg(asset_symbol)
  AND asset_networks.network_code = sqlc.arg(network_code)
  AND networks.active;

-- name: ListVenues :many
SELECT *
FROM crypto.venues
WHERE enabled
ORDER BY venue_code;

-- name: GetActivePolicy :one
SELECT *
FROM crypto.policies
WHERE active
  AND effective_at <= clock_timestamp()
ORDER BY effective_at DESC
LIMIT 1;

-- name: GetPolicy :one
SELECT *
FROM crypto.policies
WHERE policy_version = sqlc.arg(policy_version);

-- name: GetCustomerProfile :one
SELECT *
FROM crypto.customer_profiles
WHERE customer_reference = sqlc.arg(customer_reference);

-- name: GetCustomerProfileForUpdate :one
SELECT *
FROM crypto.customer_profiles
WHERE customer_reference = sqlc.arg(customer_reference)
FOR UPDATE;

-- name: CreateCustomerProfile :one
INSERT INTO crypto.customer_profiles (
    customer_reference,
    paper_account_id,
    cash_ledger_account_id,
    policy_version
) VALUES (
    sqlc.arg(customer_reference),
    sqlc.arg(paper_account_id),
    sqlc.arg(cash_ledger_account_id),
    sqlc.arg(policy_version)
)
ON CONFLICT (customer_reference) DO NOTHING
RETURNING *;

-- name: GetAssetLedgerAccounts :one
SELECT *
FROM crypto.asset_ledger_accounts
WHERE customer_reference = sqlc.arg(customer_reference)
  AND asset_symbol = sqlc.arg(asset_symbol);

-- name: InsertAssetLedgerAccounts :one
INSERT INTO crypto.asset_ledger_accounts (
    customer_reference,
    asset_symbol,
    customer_ledger_account_id,
    custody_inventory_ledger_account_id
) VALUES (
    sqlc.arg(customer_reference),
    sqlc.arg(asset_symbol),
    sqlc.arg(customer_ledger_account_id),
    sqlc.arg(custody_inventory_ledger_account_id)
)
ON CONFLICT (customer_reference, asset_symbol) DO NOTHING
RETURNING *;

-- name: InsertConversionRequest :one
INSERT INTO crypto.conversion_requests (
    customer_reference,
    idempotency_key,
    request_hash,
    conversion_id
) VALUES (
    sqlc.arg(customer_reference),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_hash),
    sqlc.arg(conversion_id)
)
ON CONFLICT (customer_reference, idempotency_key) DO NOTHING
RETURNING conversion_id;

-- name: GetConversionRequest :one
SELECT *
FROM crypto.conversion_requests
WHERE customer_reference = sqlc.arg(customer_reference)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateConversion :one
INSERT INTO crypto.conversions (
    id,
    customer_reference,
    source_asset,
    destination_asset,
    source_amount,
    simulation_scenario,
    policy_version,
    reservation_ledger_transaction_id
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(source_asset),
    sqlc.arg(destination_asset),
    sqlc.arg(source_amount),
    sqlc.arg(simulation_scenario),
    sqlc.arg(policy_version),
    sqlc.arg(reservation_ledger_transaction_id)
)
RETURNING *;

-- name: GetConversion :one
SELECT *
FROM crypto.conversions
WHERE id = sqlc.arg(id);

-- name: GetConversionForUpdate :one
SELECT *
FROM crypto.conversions
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: CompleteConversion :one
UPDATE crypto.conversions
SET status = sqlc.arg(status),
    reason_code = sqlc.narg(reason_code),
    next_action = sqlc.arg(next_action),
    routing_metadata = sqlc.arg(routing_metadata),
    compliance_case_id = sqlc.narg(compliance_case_id)
WHERE id = sqlc.arg(id)
  AND status = 'ROUTING'
RETURNING *;

-- name: ListConversions :many
SELECT *
FROM crypto.conversions
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: InsertLeg :one
INSERT INTO crypto.legs (
    id,
    conversion_id,
    leg_sequence,
    side,
    asset_symbol,
    input_amount,
    filled_quantity,
    reference_price,
    average_execution_price,
    gross_usd,
    venue_fee_usd,
    platform_fee_usd,
    final_customer_usd,
    price_improvement_usd,
    status,
    failure_code,
    ledger_transaction_id
) VALUES (
    sqlc.arg(id),
    sqlc.arg(conversion_id),
    sqlc.arg(leg_sequence),
    sqlc.arg(side),
    sqlc.arg(asset_symbol),
    sqlc.arg(input_amount),
    sqlc.arg(filled_quantity),
    sqlc.arg(reference_price),
    sqlc.arg(average_execution_price),
    sqlc.arg(gross_usd),
    sqlc.arg(venue_fee_usd),
    sqlc.arg(platform_fee_usd),
    sqlc.arg(final_customer_usd),
    sqlc.arg(price_improvement_usd),
    sqlc.arg(status),
    sqlc.narg(failure_code),
    sqlc.narg(ledger_transaction_id)
)
RETURNING *;

-- name: ListLegs :many
SELECT *
FROM crypto.legs
WHERE conversion_id = sqlc.arg(conversion_id)
ORDER BY leg_sequence;

-- name: InsertChildOrder :one
INSERT INTO crypto.child_orders (
    id,
    leg_id,
    child_sequence,
    venue_code,
    client_order_id,
    provider_order_id,
    requested_quantity,
    filled_quantity,
    quote_bid,
    quote_ask,
    venue_fee_rate,
    effective_unit_price,
    execution_price,
    gross_usd,
    venue_fee_usd,
    status,
    failure_code,
    quote_observed_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(leg_id),
    sqlc.arg(child_sequence),
    sqlc.arg(venue_code),
    sqlc.arg(client_order_id),
    sqlc.narg(provider_order_id),
    sqlc.arg(requested_quantity),
    sqlc.arg(filled_quantity),
    sqlc.arg(quote_bid),
    sqlc.arg(quote_ask),
    sqlc.arg(venue_fee_rate),
    sqlc.arg(effective_unit_price),
    sqlc.arg(execution_price),
    sqlc.arg(gross_usd),
    sqlc.arg(venue_fee_usd),
    sqlc.arg(status),
    sqlc.narg(failure_code),
    sqlc.arg(quote_observed_at)
)
RETURNING *;

-- name: ListChildOrders :many
SELECT child_orders.*
FROM crypto.child_orders AS child_orders
JOIN crypto.legs AS legs ON legs.id = child_orders.leg_id
WHERE legs.conversion_id = sqlc.arg(conversion_id)
ORDER BY legs.leg_sequence, child_orders.child_sequence;

-- name: InsertFill :one
INSERT INTO crypto.fills (
    id,
    child_order_id,
    venue_code,
    external_fill_id,
    quantity,
    price,
    gross_usd,
    venue_fee_usd,
    occurred_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(child_order_id),
    sqlc.arg(venue_code),
    sqlc.arg(external_fill_id),
    sqlc.arg(quantity),
    sqlc.arg(price),
    sqlc.arg(gross_usd),
    sqlc.arg(venue_fee_usd),
    sqlc.arg(occurred_at)
)
RETURNING *;

-- name: ListFills :many
SELECT fills.*
FROM crypto.fills AS fills
JOIN crypto.child_orders AS child_orders ON child_orders.id = fills.child_order_id
JOIN crypto.legs AS legs ON legs.id = child_orders.leg_id
WHERE legs.conversion_id = sqlc.arg(conversion_id)
ORDER BY legs.leg_sequence, child_orders.child_sequence, fills.occurred_at, fills.id;

-- name: SumRecentCryptoToUSD :one
SELECT COALESCE(SUM(legs.final_customer_usd), 0)::NUMERIC(38, 18) AS total
FROM crypto.legs AS legs
JOIN crypto.conversions AS conversions ON conversions.id = legs.conversion_id
WHERE conversions.customer_reference = sqlc.arg(customer_reference)
  AND legs.side = 'SELL'
  AND legs.created_at >= sqlc.arg(since_time)
  AND legs.status IN ('FILLED', 'PARTIALLY_FILLED');

-- name: ListPortfolioBalances :many
SELECT
    accounts.asset_symbol,
    COALESCE(settled.debit_balance, 0)::NUMERIC(38, 18) AS settled,
    COALESCE(held.debit_balance, 0)::NUMERIC(38, 18) AS held,
    COALESCE(frozen.debit_balance, 0)::NUMERIC(38, 18) AS frozen
FROM crypto.asset_ledger_accounts AS accounts
LEFT JOIN ledger.account_balances AS settled
    ON settled.account_id = accounts.customer_ledger_account_id
    AND settled.currency = accounts.asset_symbol
    AND settled.balance_dimension = 'SETTLED'
LEFT JOIN ledger.account_balances AS held
    ON held.account_id = accounts.customer_ledger_account_id
    AND held.currency = accounts.asset_symbol
    AND held.balance_dimension = 'HELD'
LEFT JOIN ledger.account_balances AS frozen
    ON frozen.account_id = accounts.customer_ledger_account_id
    AND frozen.currency = accounts.asset_symbol
    AND frozen.balance_dimension = 'FROZEN'
WHERE accounts.customer_reference = sqlc.arg(customer_reference)
ORDER BY accounts.asset_symbol;

-- name: InsertDepositAddressRequest :one
INSERT INTO crypto.deposit_address_requests (
    customer_reference,
    idempotency_key,
    request_hash,
    deposit_address_id
) VALUES (
    sqlc.arg(customer_reference),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_hash),
    sqlc.arg(deposit_address_id)
)
ON CONFLICT (customer_reference, idempotency_key) DO NOTHING
RETURNING deposit_address_id;

-- name: GetDepositAddressRequest :one
SELECT *
FROM crypto.deposit_address_requests
WHERE customer_reference = sqlc.arg(customer_reference)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateDepositAddress :one
INSERT INTO crypto.deposit_addresses (
    id,
    customer_reference,
    asset_symbol,
    network_code,
    provider,
    external_address,
    memo
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(asset_symbol),
    sqlc.arg(network_code),
    sqlc.arg(provider),
    sqlc.arg(external_address),
    sqlc.narg(memo)
)
ON CONFLICT (customer_reference, asset_symbol, network_code) DO NOTHING
RETURNING *;

-- name: GetDepositAddress :one
SELECT *
FROM crypto.deposit_addresses
WHERE id = sqlc.arg(id);

-- name: GetDepositAddressByAssetNetwork :one
SELECT *
FROM crypto.deposit_addresses
WHERE customer_reference = sqlc.arg(customer_reference)
  AND asset_symbol = sqlc.arg(asset_symbol)
  AND network_code = sqlc.arg(network_code);

-- name: ListDepositAddresses :many
SELECT *
FROM crypto.deposit_addresses
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC;

-- name: CreateDeposit :one
INSERT INTO crypto.deposits (
    id,
    customer_reference,
    deposit_address_id,
    asset_symbol,
    network_code,
    quantity,
    status,
    provider,
    provider_transaction_id,
    ledger_transaction_id,
    reason_code
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(deposit_address_id),
    sqlc.arg(asset_symbol),
    sqlc.arg(network_code),
    sqlc.arg(quantity),
    sqlc.arg(status),
    sqlc.arg(provider),
    sqlc.arg(provider_transaction_id),
    sqlc.narg(ledger_transaction_id),
    sqlc.narg(reason_code)
)
RETURNING *;

-- name: GetDeposit :one
SELECT *
FROM crypto.deposits
WHERE id = sqlc.arg(id);

-- name: ListDeposits :many
SELECT *
FROM crypto.deposits
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: InsertWithdrawalAddressRequest :one
INSERT INTO crypto.withdrawal_address_requests (
    customer_reference,
    idempotency_key,
    request_hash,
    address_id
) VALUES (
    sqlc.arg(customer_reference),
    sqlc.arg(idempotency_key),
    sqlc.arg(request_hash),
    sqlc.arg(address_id)
)
ON CONFLICT (customer_reference, idempotency_key) DO NOTHING
RETURNING address_id;

-- name: GetWithdrawalAddressRequest :one
SELECT *
FROM crypto.withdrawal_address_requests
WHERE customer_reference = sqlc.arg(customer_reference)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateWithdrawalAddress :one
INSERT INTO crypto.withdrawal_addresses (
    id,
    customer_reference,
    asset_symbol,
    network_code,
    external_address,
    label,
    status,
    risk_level,
    cooling_until,
    cooling_reason,
    policy_version
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(asset_symbol),
    sqlc.arg(network_code),
    sqlc.arg(external_address),
    sqlc.arg(label),
    sqlc.arg(status),
    sqlc.arg(risk_level),
    sqlc.narg(cooling_until),
    sqlc.arg(cooling_reason),
    sqlc.arg(policy_version)
)
RETURNING *;

-- name: GetWithdrawalAddress :one
SELECT *
FROM crypto.withdrawal_addresses
WHERE id = sqlc.arg(id);

-- name: GetWithdrawalAddressForUpdate :one
SELECT *
FROM crypto.withdrawal_addresses
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListWithdrawalAddresses :many
SELECT *
FROM crypto.withdrawal_addresses
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC;

-- name: TransitionWithdrawalAddress :one
UPDATE crypto.withdrawal_addresses
SET status = sqlc.arg(status),
    risk_level = sqlc.arg(risk_level),
    cooling_until = sqlc.narg(cooling_until),
    cooling_reason = sqlc.arg(cooling_reason)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertWithdrawalAddressEvent :one
INSERT INTO crypto.withdrawal_address_events (
    address_id,
    from_status,
    to_status,
    actor_type,
    actor_id,
    reason_code,
    metadata
) VALUES (
    sqlc.arg(address_id),
    sqlc.narg(from_status),
    sqlc.arg(to_status),
    sqlc.arg(actor_type),
    sqlc.arg(actor_id),
    sqlc.arg(reason_code),
    sqlc.arg(metadata)
)
RETURNING *;

-- name: InsertWithdrawalRequest :one
INSERT INTO crypto.withdrawal_requests (
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
FROM crypto.withdrawal_requests
WHERE customer_reference = sqlc.arg(customer_reference)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: CreateWithdrawal :one
INSERT INTO crypto.withdrawals (
    id,
    customer_reference,
    withdrawal_address_id,
    asset_symbol,
    network_code,
    quantity,
    status,
    reason_code,
    next_action,
    provider,
    provider_withdrawal_id,
    reservation_ledger_transaction_id,
    policy_version
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(withdrawal_address_id),
    sqlc.arg(asset_symbol),
    sqlc.arg(network_code),
    sqlc.arg(quantity),
    sqlc.arg(status),
    sqlc.arg(reason_code),
    sqlc.arg(next_action),
    sqlc.arg(provider),
    sqlc.arg(provider_withdrawal_id),
    sqlc.arg(reservation_ledger_transaction_id),
    sqlc.arg(policy_version)
)
RETURNING *;

-- name: GetWithdrawal :one
SELECT *
FROM crypto.withdrawals
WHERE id = sqlc.arg(id);

-- name: GetWithdrawalForUpdate :one
SELECT *
FROM crypto.withdrawals
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ListWithdrawals :many
SELECT *
FROM crypto.withdrawals
WHERE customer_reference = sqlc.arg(customer_reference)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: TransitionWithdrawal :one
UPDATE crypto.withdrawals
SET status = sqlc.arg(status),
    reason_code = sqlc.arg(reason_code),
    next_action = sqlc.arg(next_action),
    broadcast_ledger_transaction_id = sqlc.narg(broadcast_ledger_transaction_id),
    reservation_reversal_transaction_id = sqlc.narg(reservation_reversal_transaction_id),
    broadcast_reversal_transaction_id = sqlc.narg(broadcast_reversal_transaction_id)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertProviderEvent :one
INSERT INTO crypto.provider_events (
    provider,
    external_event_id,
    resource_type,
    resource_id,
    event_type,
    payload_hash,
    domain_record_id
) VALUES (
    sqlc.arg(provider),
    sqlc.arg(external_event_id),
    sqlc.arg(resource_type),
    sqlc.arg(resource_id),
    sqlc.arg(event_type),
    sqlc.arg(payload_hash),
    sqlc.arg(domain_record_id)
)
ON CONFLICT (provider, external_event_id) DO NOTHING
RETURNING *;

-- name: GetProviderEvent :one
SELECT *
FROM crypto.provider_events
WHERE provider = sqlc.arg(provider)
  AND external_event_id = sqlc.arg(external_event_id);

-- name: CreateReconciliationRun :one
INSERT INTO crypto.reconciliation_runs (
    id,
    customer_reference,
    asset_symbol,
    ledger_amount,
    custody_amount,
    difference,
    status,
    compliance_case_id,
    policy_version
) VALUES (
    sqlc.arg(id),
    sqlc.arg(customer_reference),
    sqlc.arg(asset_symbol),
    sqlc.arg(ledger_amount),
    sqlc.arg(custody_amount),
    sqlc.arg(difference),
    sqlc.arg(status),
    sqlc.narg(compliance_case_id),
    sqlc.arg(policy_version)
)
RETURNING *;
