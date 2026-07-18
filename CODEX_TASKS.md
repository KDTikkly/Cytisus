# Cytisus Codex Task Plan

本文件把 PDM 拆为适合 Codex Chat 分阶段执行的任务。每个任务默认创建功能分支和 Draft PR。

---

## Epic 0 — Repository Foundation

### T0.1 Monorepo skeleton
交付：
- Go workspace；
- `apps/api`、`apps/worker`；
- Next.js `apps/web`、`apps/admin-web`；
- SwiftUI `apps/ios`；
- `contracts/openapi`；
- `deploy/docker-compose.yml`；
- `Makefile`；
- 基础 README。

验收：
- `make bootstrap`
- `make compose-up`
- API health；
- Web/Admin 打开；
- iOS build；
- CI 绿。

### T0.2 CI and security
交付：
- Go fmt/vet/test；
- sqlc generation check；
- migration check；
- Web lint/typecheck/test；
- iOS build；
- OpenAPI diff；
- Secret scan；
- container build；
- Draft PR template；
- CODEOWNERS 示例；
- branch protection 文档。

---

## Epic 1 — Identity and Customer

### T1.1 Local identity simulator
- 注册；
- 登录；
- Passkey mock；
- Apple/Google subject mock；
- token expiry；
- identity conflict；
- account linking；
- session revoke。

### T1.2 Customer/device/capability
- Customer；
- LoginIdentity；
- DeviceSession；
- CapabilityDecision；
- reason codes；
- policy version；
- Web/iOS capability endpoint。

### T1.3 Audit foundation
- immutable audit events；
- actor；
- correlation；
- sensitive access audit；
- admin viewer。

---

## Epic 2 — Ledger and Async Core

### T2.1 Ledger schema
- chart of accounts；
- transactions；
- entries；
- holds；
- balance projection；
- immutable constraints；
- reversal。

### T2.2 Money package
- Decimal；
- currency；
- rounding；
- JSON string；
- DB mapping；
- no-float lint/test。

### T2.3 Idempotency and provider events
- request idempotency；
- provider event unique keys；
- replay safety。

### T2.4 Transactional outbox
- outbox table；
- worker claim；
- retry；
- DLQ；
- replay；
- admin viewer。

### T2.5 Ledger invariants
- balanced postings；
- concurrent command tests；
- duplicate event tests；
- crash recovery tests。

---

## Epic 3 — Market Data and Paper Securities

### T3.1 Instrument master
- full fixture catalog；
- types；
- capability matrix；
- search；
- symbol changes。

### T3.2 Market data
- quote types；
- stale handling；
- delayed/simulated；
- market status；
- replay。

### T3.3 Paper broker
- Market/Limit；
- DAY/GTC；
- partial fills；
- fractional；
- cancel/reject/expire；
- deterministic mode。

### T3.4 Orders/positions/settlement
- parent order；
- child/fills abstraction；
- provisional buying power；
- settled/withdrawable separation；
- fee policy hooks。

### T3.5 Web trading surfaces
- Trading；
- Portfolio；
- Research；
- quote badge；
- order ticket；
- status/error states。

### T3.6 iOS base
- Home；
- Markets；
- Portfolio；
- Asset detail；
- Buy/Sell flow。

---

## Epic 4 — Progressive KYC and Banking

### T4.1 Onboarding simulator
- REGISTERED；
- BASIC_KYC；
- INVESTMENT_ELIGIBLE；
- ENHANCED_REVIEW；
- capability updates。

### T4.2 Compliance case system
- case types；
- evidence；
- assignment；
- decision；
- maker-checker；
- audit。

### T4.3 Linked bank accounts
- ACH/Wire；
- ownership；
- instant/micro/bank document；
- masks；
- capability。

### T4.4 ACH simulator
- deposit；
- provisional buying power；
- settle；
- return；
- withdrawal；
- return codes。

### T4.5 Wire simulator
- instructions；
- funds detected；
- name mismatch；
- missing reference；
- credit/return。

### T4.6 Closed-loop withdrawal
- preferred source account；
- new same-name enhanced review；
- cooling；
- third-party rejection；
- AML reasons；
- admin case。

---

## Epic 5 — Crypto

### T5.1 Asset/network registry
- main asset list；
- network matrix；
- native contract identifiers；
- capability states；
- maintenance。

### T5.2 Custody simulator
- deposit addresses；
- confirmations；
- deposit credit；
- withdrawal broadcast；
- failure/reorg fixture；
- no private keys in DB。

### T5.3 Address whitelist
- proof mock；
- risk；
- dynamic cooling；
- active/suspended/revoked；
- re-auth。

### T5.4 USD conversion and review
- stablecoin markets；
- depeg fixture；
- Crypto → USD risk review；
- threshold policy；
- held/settled cash。

### T5.5 Venue simulators and SOR
- three venues；
- quotes；
- net price；
- split plan；
- child orders；
- timeout；
- partial fill；
- price improvement pass-through。

### T5.6 Crypto UI
- asset list；
- USD pairs；
- deposit；
- withdraw；
- convert；
- network selection；
- QR secondary only。

---

## Epic 6 — Card

### T6.1 Card domain
- virtual card；
- lifecycle；
- PIN；
- freeze；
- replacement；
- controls。

### T6.2 Card processing simulator
- auth；
- hold；
- capture；
- partial；
- reversal；
- refund；
- tip；
- duplicate；
- offline；
- dispute。

### T6.3 FX
- merchant currency；
- auth/clearing rate；
- USD settlement；
- markup policy；
- disclosure。

### T6.4 Spending power
- eligible assets；
- haircuts；
- concentration；
- stale/halt exclusion；
- explainability；
- caps。

### T6.5 Repayment/Auto-Sell
- three modes；
- mandate；
- priority；
- min keep；
- daily max；
- fractional permission；
- protected limit；
- next asset；
- freeze on failure。

### T6.6 Physical cards
- application；
- review；
- manufacturing；
- shipping；
- activation；
- lost/stolen/replacement；
- wallet availability placeholder。

---

## Epic 7 — RWA

### T7.1 Local Base
- Anvil container；
- deployment script；
- permissioned token；
- whitelist；
- freeze；
- forced redemption；
- events。

### T7.2 RWA Vault
- platform-managed address；
- mint request；
- lock whole share；
- chain finality；
- supply reconciliation。

### T7.3 External permissioned address
- manual address；
- proof mock；
- identity/risk；
- cooling；
- transfer；
- suspend/revoke。

### T7.4 Redemption
- receive/freeze；
- burn；
- release share；
- error recovery；
- reconciliation.

### T7.5 Corporate actions
- cash dividend；
- withholding；
- split；
- reverse split；
- symbol change；
- merger/retirement fixtures；
- external address snapshot。

---

## Epic 8 — Membership, Pricing, Reporting

### T8.1 Membership
- Standard/Premium/Metal；
- paid/asset waiver/community grant；
- entitlement source；
- validity；
- physical card separation。

### T8.2 Pricing
- fixed + bps；
- policy version；
- parent-order fixed fee once；
- partial fills；
- price improvement；
- revenue ledger。

### T8.3 Metal allowance
- monthly executed notional；
- buy/sell count；
- boundary split；
- proration；
- upgrade/downgrade anti-gaming。

### T8.4 Reporting
- activity ledger；
- monthly/annual；
- trades；
- tax lots；
- dividends；
- bank/chain/card/RWA；
- CSV/PDF。

### T8.5 Account closure
- preconditions；
- export；
- capability closure；
- data deletion/anonymization；
- legal hold retention。

---

## Epic 9 — Security and Admin Hardening

### T9.1 Account recovery
- evidence；
- liveness mock；
- history comparison；
- approval；
- session revoke；
- 72h hold；
- dual approval override。

### T9.2 Read-only mode
- global capability enforcement；
- view-only UX；
- Card freeze；
- no withdrawal；
- no profile changes。

### T9.3 Protective sell
- strong re-auth；
- case approval；
- max quantity；
- expiration；
- protected limit；
- frozen proceeds。

### T9.4 Admin RBAC
- roles；
- permissions；
- maker-checker；
- emergency freeze；
- unfreeze dual approval；
- sensitive access reasons。

### T9.5 Security controls
- KMS abstraction；
- Secrets；
- log redaction；
- rate limit；
- webhook signatures；
- dependency and SAST；
- environment guard。

---

## Epic 10 — Production Readiness without Real Provider Activation

### T10.1 Provider contract suites
- shared contract tests；
- health；
- timeout；
- error taxonomy；
- idempotency。

### T10.2 Deployment
- AWS reference Terraform or documented IaC；
- network segmentation；
- staging；
- backups；
- restore test；
- observability。

### T10.3 Operational runbooks
- ledger break；
- custody mismatch；
- broker mismatch；
- bank return；
- Card outage；
- chain halt；
- security takeover；
- provider switch；
- rollback。

### T10.4 Final E2E
Run every PDM Definition of Done flow with synthetic personas and produce an evidence report.
