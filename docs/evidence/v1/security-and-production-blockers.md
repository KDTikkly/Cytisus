# Security findings and production blockers

## Production-readiness decision

**Blocked.** No code or test result can approve the non-code dependencies below, and the repository contains no separate human approvals for them.

## Product security gaps visible from the DoD audit

- Production authentication is not implemented: fixture access tokens are synthetic, and Passkey login/recovery is absent.
- Account recovery and server-enforced Read-only Security Mode are absent.
- Progressive KYC/investment eligibility is absent.
- Several passing backend financial flows lack equivalent Web, key iOS, or Admin enforcement surfaces.
- RWA transfer lacks an application/API workflow; only contract-level whitelist transfer behavior exists.
- Report/CSV/PDF export and protective-sell workflows are absent.
- The local simulator does not establish production rate limiting, enterprise Admin SSO, production secret/KMS handling, real webhook key rotation, or production incident response.

These gaps are release blockers, not automatically classified vulnerabilities in the local-only simulator.

## Codex Security scan

The repository-wide scan ranked 584 source-like files and completed full-file review receipts for all 39 files selected by the standard top-30% risk lane. The following findings survived source/control/sink validation and remain release blockers:

- privileged Admin routes trust caller-supplied `X-Admin-ID` and `X-Admin-Role` headers; CORS does not authenticate direct HTTP clients;
- local/test Bank and Crypto simulator callback routes do not authenticate the callback origin;
- a successfully funded new same-name bank account can bypass its still-active cooling period;
- Card Auto-Sell price improvement can cause actual proceeds to exceed the user's daily authorization cap;
- RWA dividends use processing-time holdings instead of a record-date ownership snapshot;
- an outcome-unknown RWA mint can release its underlying shares before chain non-execution is conclusive;
- an open Paper sell order can later fill against shares already locked for RWA, while reconciliation can still appear balanced;
- cross-module cash spend checks do not share a common Ledger-account lock, permitting an adverse concurrent-spend interleaving;
- exact Card provider-event replay paths use global getters before customer ownership validation;
- pending Outbox event identity/payload and deletion are not protected by the immutable-event trigger used elsewhere.

Provider-contract-dependent late bank returns and wire-exception transitions remain deferred. Missing KYC, recovery, read-only mode, and capability enforcement are recorded as incomplete product controls rather than represented as completed security features.

## External dependencies requiring separate human approval

- legal entity, product structure, jurisdictions, customer agreements, disclosures, and regulatory analysis;
- securities broker-dealer/clearing relationship and live-account approval;
- sponsor bank, ACH operator, USD Wire provider, closed-loop withdrawal policy, and bank compliance approval;
- KYC/KYB, AML, sanctions, fraud, device-risk, and transaction-monitoring vendors and operating procedures;
- crypto custody, liquidity venues, licensing/registration analysis, chain analytics, withdrawal policy, and Travel Rule obligations where applicable;
- card issuer, sponsor bank, network/program manager, PCI scope, dispute operations, and Apple/Google Wallet agreements and entitlements;
- RWA issuer/SPV/custodian/legal characterization, transfer-agent responsibilities, permissioned holder rules, corporate actions, tax, and real chain deployment approval;
- licensed market-data agreements and truthful entitlement/status mapping;
- production cloud accounts, network isolation, KMS/HSM, secrets, backups, disaster recovery, observability, security monitoring, penetration testing, and incident response;
- approved fee schedules, limits, risk thresholds, accounting policy, reconciliation tolerances, tax forms, retention, privacy, and customer-support procedures.

Until each applicable dependency is documented as human-approved, Cytisus v1 must remain a local synthetic simulator and must not handle real money, real securities, real crypto, real card credentials, or real customer identity data.
