# Automated test evidence

## Explicit 20-flow runner

Command:

```text
go test '-tags=integration,evidence' -run '^TestMVPDefinitionOfDoneEvidence$' -count=1 -v ./tests/integration
```

Result: process `PASS`; 20 named subtests comprised 12 `PASS` and 8 reasoned `SKIP` results. The skipped group maps to 2 partial, 5 missing, and 1 externally blocked DoD requirement. See [the matrix](mvp-dod-matrix.md) for the product verdict.

## PostgreSQL integration suite

Command:

```text
go test -tags=integration -count=1 ./tests/integration
```

Result: `PASS` (`ok ... 10.006s`) against isolated PostgreSQL 17.6 and Redis 8.2.1.

Targeted evidence tests also passed independently:

- `TestACHWireWithdrawalComplianceAndReconciliation`
- `TestCryptoRoutingCustodyReviewAndReconciliation`
- `TestCardAPIEndToEnd`
- `TestRwaMintDividendBurnRecoveryAndInvariants`
- `TestRwaExternalAddressProofCoolingAndExternalCustody`

## Browser flows

The current Web/Admin source was served against the current local API. Automated browser actions created only synthetic personas and completed:

- fixture registration;
- AAPL simulated order and portfolio refresh;
- same-name synthetic bank account link with verification-pending state;
- USD→BTC simulated route with explicit fees and price improvement;
- virtual Card creation and activation;
- bank reconciliation difference creation and Admin case queue load.

The only observed browser console 404 was the optional site icon request; required application requests used successful responses.

## Final repository gates

The Windows evidence host does not have GNU Make. The exact underlying commands from the Makefile were executed individually, with these results:

| Gate              | Result                 | Evidence                                                                                                                                              |
| ----------------- | ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| Generate          | PASS                   | Local `sqlc v1.31.1` completed `sqlc generate -f db/sqlc.yaml` with no generated diff.                                                                |
| Format            | PASS                   | `gofmt` plus `npm run format`; `git diff --check` and `npm run format:check` passed.                                                                  |
| Lint/dependencies | PASS                   | `go vet`, `go run ./tools/financecheck`, ESLint, TypeScript typecheck, and `npm audit --audit-level=moderate` passed; npm reported 0 vulnerabilities. |
| Unit/contract     | PASS                   | All Go package tests, 13 Vitest tests, and 6 Foundry contract tests passed.                                                                           |
| Integration       | PASS                   | The complete PostgreSQL integration package passed in 10.065 seconds; the explicit 20-flow evidence runner then passed in 6.114 seconds.              |
| Migration         | PASS                   | `go run ./tools/migratecheck` passed.                                                                                                                 |
| OpenAPI           | PASS                   | Redocly validated the OpenAPI contract with no error.                                                                                                 |
| Secret scan       | PASS                   | Local Gitleaks scanned approximately 2.62 MB and found no leaks.                                                                                      |
| Build             | PASS (host-applicable) | All Go applications and both Next.js applications built. The Windows host cannot run Xcode; the macOS CI job is the authoritative iOS build evidence. |

The Draft PR Linux/macOS CI will execute the Make targets themselves and is the final independent gate before integration to `main`.
