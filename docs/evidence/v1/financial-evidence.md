# Financial, audit, provider, and reconciliation evidence

All evidence below came from read-only SQL after a synthetic integration test. Decimal values are PostgreSQL `NUMERIC`; no floating-point balance assertion was used.

## Ledger balance proof

Grouped debit-credit imbalance was `0.000000000000000000` for every sampled transaction type. Examples include:

| Domain  | Transaction types observed                                                                                                                                                               | Financial result                                                                                                     |
| ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| Banking | `ACH_FUNDING_PENDING`, `ACH_FUNDING_SETTLED`, `ACH_PENDING_RELEASED`, `BANK_WITHDRAWAL_RESERVED`, `BANK_WITHDRAWAL_SUBMITTED`, `REVERSAL`                                                | Debits equaled credits for every group. ACH settlement and return used posting/reversal rather than balance updates. |
| Crypto  | `CRYPTO_BUY`, `CRYPTO_SELL`, `CRYPTO_CONVERSION_RESERVATION`, `CRYPTO_RESERVATION_RELEASE`, `CRYPTO_DEPOSIT`, `CRYPTO_WITHDRAWAL_RESERVATION`, `CRYPTO_WITHDRAWAL_BROADCAST`, `REVERSAL` | Every group imbalance was zero, including failed-withdrawal recovery.                                                |
| Card    | `CARD_AUTHORIZATION_HOLD`, `CARD_AUTHORIZATION_RELEASE`, `CARD_CAPTURE_RECEIVABLE`, `CARD_CAPTURE_REPAYMENT`, `CARD_REFUND`, `CARD_DISPUTE_CREDIT`, `CARD_STATEMENT_REPAYMENT`           | Every group imbalance was zero.                                                                                      |
| RWA     | `RWA_UNDERLYING_LOCKED`, `RWA_UNDERLYING_RELEASED`, `RWA_CASH_DIVIDEND`                                                                                                                  | Every group imbalance was zero; redemption released shares only after burn confirmation.                             |

## Audit evidence

Representative immutable audit actions observed:

- Banking: `bank.funding.settled`, `bank.funding.returned`, `bank.funding.name_mismatch`, `bank.withdrawal.settled`, `bank.reconciliation.completed`, `ledger.transaction.reversed`.
- Crypto: `crypto.conversion.completed`, `crypto.deposit.confirmed`, `crypto.withdrawal.broadcast`, `crypto.withdrawal.failed`, `crypto.reconciliation.completed`.
- Card: `card.authorization.approved`, `card.authorization.declined`, `card.authorization.reversed`, `card.capture.posted`, `card.refund.posted`, `card.dispute.resolved`, `card.reconciliation.completed`.
- RWA: `rwa.mint.underlying_locked`, `rwa.mint.minted`, `rwa.redemption.completed`, `rwa.dividend.usd_cash_credited`, `rwa.reconciliation.completed`.

Actors were retained as `USER`, `ADMIN`, `SYSTEM`, or `PROVIDER`. Financial commands also emitted pending transactional Outbox events in the same test database.

## Provider simulator evidence

Provider-event uniqueness tables retained these synthetic callbacks:

| Provider                    | Accepted event types                                                                                                            |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `local-bank-simulator`      | ownership verified, ACH processing/settled/returned, wire funds detected/name mismatch, withdrawal submitted/processing/settled |
| `local-custody-simulator`   | deposit confirmed, withdrawal broadcast/confirmed/failed                                                                        |
| `local-card-simulator`      | authorization, capture, reversal, refund                                                                                        |
| `local-anvil-rwa-simulator` | mint confirmed, burn confirmed                                                                                                  |

Duplicate callback and replay behavior is asserted by the integration suite through `(provider, external_event_id)` uniqueness and idempotent application-service paths.

## Reconciliation evidence

- Banking matched run: Ledger `300`, provider `300`, difference `0`, status `COMPLETED_WITHOUT_DIFFERENCE`.
- Crypto matched run: BTC Ledger `0.729576498511876156`, custody equal, difference `0`; a separate difference run retained `0.729576498511876156` without auto-adjustment.
- Card matched run: hold `3.05` on Ledger and provider, receivable `0` on both, difference `0`.
- RWA daily run: chain supply `0`, locked shares `0`, difference `0`, observed block `0x1`, status `BALANCED`.
- Admin screenshot evidence intentionally created a synthetic bank difference (`ledger 0`, `provider 1`, `difference -1`) and showed the resulting `TRANSACTION_MONITORING_ALERT` with maker-checker next action.

No reconciliation path directly changed a customer balance.
