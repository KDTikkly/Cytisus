# Codex Chat Bootstrap Prompts

以下提示词适合逐段粘贴到 Codex Chat。不要一次要求 Codex 实现整个 PDM。

---

## Prompt 1 — Repository inspection and Phase 0 plan

```text
You are working on Cytisus, a regulated-finance simulation-first product.

Read these files in full:
- PDM.md
- AGENTS.md
- DECISION_REGISTER.md
- CODEX_TASKS.md

Do not implement product features yet.

First:
1. Summarize the locked architectural constraints.
2. Identify any contradictions between the repository and the documents.
3. Propose a Phase 0 repository plan.
4. List the exact files and directories you will create or change.
5. Define the local Docker Compose services.
6. Define the Makefile commands and CI jobs.
7. Identify decisions that require human approval.
8. Do not invent production providers, legal approvals, fees, or secrets.

Then implement only the approved Phase 0 foundation on a feature branch.
Run all available checks.
Commit the changes.
Push the feature branch and create a Draft PR.
Never merge main and never deploy production.
```

---

## Prompt 2 — Ledger foundation

```text
Read PDM.md sections Ledger, Database, Transactional Outbox, Testing, and AGENTS.md.

Implement Epic 2 in small reviewable commits:
- Decimal money package with no float usage
- PostgreSQL ledger schema
- balanced immutable entries
- reversal workflow
- balance projections
- request idempotency
- provider event idempotency
- transactional outbox
- worker claim/retry/DLQ
- ledger invariant and concurrency tests

Use Go, pgx, sqlc, PostgreSQL, and explicit SQL.
Do not use an ORM.
Do not directly update balances.
Every financial mutation must produce audit information and an outbox event where applicable.

Before coding, show:
- posting model
- transaction boundaries
- locking strategy
- schema constraints
- failure and replay behavior

After implementation:
- run format, lint, unit, integration, migration, sqlc, secret scan, and build checks
- commit
- push the feature branch
- update the Draft PR
- do not merge
```

---

## Prompt 3 — Paper securities vertical slice

```text
Implement one complete vertical slice for Paper Securities:
registration fixture → paper account → instrument search → simulated quote → market/limit order → fill → ledger → position → portfolio UI → audit.

Follow PDM and AGENTS.md.

Requirements:
- Common stocks and ETFs are tradable
- Other U.S. listed instrument types are view-only
- Market and Limit orders
- DAY and GTC
- fractional quantities
- partial fills
- quote status labels
- deterministic replay mode
- no fake real-time label
- broker events cannot write balances directly
- provisional buying power is separate from settled and withdrawable cash

Add Web flows first, then an iOS skeleton consuming the same OpenAPI contract.
Add end-to-end tests and error states.
Push a Draft PR only.
```

---

## Prompt 4 — Banking and compliance

```text
Implement the simulated ACH + USD Wire funding and closed-loop bank withdrawal flows.

Locked rules:
- same-name accounts only
- third-party withdrawals are permanently disabled in MVP
- prefer an account that has successfully funded the platform
- a new same-name withdrawal account requires ownership verification, dynamic cooling, and enhanced review
- ACH unsettled funds cannot be converted to crypto, withdrawn, or used as real card cash
- all balance changes go through Ledger
- high-risk overrides use maker-checker
- user-visible errors must explain the reason and next action without exposing internal thresholds

Include:
- state machines
- compliance cases
- admin review
- duplicate callback tests
- ACH return
- wire name mismatch
- audit and reconciliation
```

---

## Prompt 5 — Crypto and smart routing

```text
Implement the simulated Crypto vertical slice.

Assets in the local simulator:
BTC, ETH, SOL, XRP, BNB, DOGE, ADA, AVAX, LINK, LTC, USDC, USDT.

Rules:
- USD is the only quote and settlement currency
- no coin-to-coin pair
- cross-asset conversion creates two explicit USD legs
- stablecoins are not fixed to 1 USD
- use three simulated liquidity venues
- route by executable net price
- support split, partial fills, timeout, stale quote, and venue failure
- pass all price improvement to the user
- fees must be explicit
- custodial model
- address whitelist with dynamic cooling
- crypto-to-USD review policy
- no leverage, lending, staking, derivatives, or hidden bridge

Create provider contract tests and full audit/ledger coverage.
```

---

## Prompt 6 — Card

```text
Implement the Card MVP simulator:
- virtual card lifecycle
- local merchant terminal / NFC simulation
- authorization, hold, capture, partial capture, reversal, refund, tip, duplicate, offline, decline, dispute
- USD settlement with original merchant currency and auth/clearing FX
- dynamic asset-backed spending power
- CASH_ONLY, CASH_THEN_AUTO_SELL, MONTHLY_STATEMENT
- pre-authorized Auto-Sell waterfall
- protected marketable limit; never fall back to unprotected market order
- physical plastic and Metal card lifecycle
- Apple/Google Wallet adapter and truthful availability state only

All financial effects must post through Ledger.
Include Web, iOS, Admin, notifications, audit, and E2E tests.
```

---

## Prompt 7 — RWA

```text
Implement the local RWA simulator on an Anvil-compatible Base environment.

Rules:
- one chain only
- permissioned token
- whole settled shares only
- one token per locked underlying share
- platform Vault by default
- external permissioned address after proof, risk review, and cooling
- whitelist transfers
- freeze, pause, mint, burn, forced redemption
- no bridge
- burn before share release
- daily supply vs locked-share reconciliation
- cash dividends go to Settled USD Cash
- no DRIP
- make no claim that this local token is a legally issued security

Implement failure recovery and contract tests.
```

---

## Prompt 8 — Final E2E and evidence

```text
Run every flow listed in PDM.md under MVP Definition of Done using synthetic personas.

Produce:
- automated test results
- screenshots for Web/Admin
- iOS test evidence
- ledger posting evidence
- audit evidence
- provider simulator evidence
- reconciliation evidence
- known limitations
- security findings
- external dependencies that block real-money production

Do not mark the product production-ready unless every non-code legal/provider dependency has been separately approved by humans.
Open or update a Draft PR. Do not merge or deploy.
```
