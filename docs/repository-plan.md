# Phase 0 repository plan

## Locked constraints

- Go modular monolith; modules do not write another module's schema or place business rules in transport/provider layers.
- PostgreSQL + pgx + sqlc + explicit SQL; no ORM or SQLite substitute for integration tests.
- PostgreSQL transactional outbox + Go worker; no Kafka/NATS in MVP.
- Web and Admin are separate Next.js applications; iOS is native SwiftUI.
- REST/OpenAPI is the client contract; money is represented as strings and never floats when financial work begins.
- Every real capability has a provider interface and a local simulator; production may never fall back to a simulator.
- No real secrets, customer data, provider claims, automatic main merge, or production deployment.

## Repository-to-document differences at inspection time

The repository had no source files, commits, configured remote, CI, service definitions, migrations, or application skeletons. There was therefore no existing implementation that contradicted a locked product decision. The missing remote blocks publishing until a human selects or creates the GitHub repository.

The Windows development host has Go and Node.js, but not Docker, GNU Make, Swift/Xcode, sqlc, or gitleaks. Tool-backed checks are pinned in the repository; Docker and iOS validation are delegated to Linux/macOS CI.

## Files and directories created or changed

```text
/.github/
  CODEOWNERS
  dependabot.yml
  pull_request_template.md
  workflows/ci.yml
/apps/
  api/
  worker/
  migrate/
  simulator/
  web/
  admin-web/
  ios/
/contracts/
  openapi/openapi.yaml
  events/README.md
  schemas/README.md
/db/
  migrations/
  queries/
  sqlc.yaml
/deploy/
  docker-compose.yml
  docker/go-app.Dockerfile
  docker/web-app.Dockerfile
/docs/
  adr/0001-phase-0-foundation.md
  branch-protection.md
  human-approvals.md
  repository-plan.md
/internal/
  foundation/
  identity/
/pkg/
  testfixtures/
/tests/integration/
/tools/migratecheck/
/.editorconfig
/.dockerignore
/.env.example
/.gitattributes
/.gitignore
/.gitleaks.toml
/.prettierignore
/Makefile
/go.mod
/go.sum
/go.work
/package.json
/package-lock.json
/README.md
```

The development-pack documents in the repository root are retained unchanged except for `README.md`, which is now the repository entry point.

## Local Docker Compose services

| Service     | Purpose                                                | External port |
| ----------- | ------------------------------------------------------ | ------------: |
| `postgres`  | PostgreSQL system of record                            |          5432 |
| `redis`     | Cache and ephemeral coordination only                  |          6379 |
| `migrate`   | Apply audited SQL migrations before applications start |          none |
| `api`       | Public API operational health skeleton                 |          8080 |
| `worker`    | Background worker lifecycle skeleton                   |          none |
| `simulator` | Explicitly simulated provider/demo identity surface    |          8090 |
| `web`       | Customer Next.js application                           |          3000 |
| `admin-web` | Isolated Admin Next.js application                     |          3001 |
| `anvil`     | Local EVM chain reserved for the RWA simulator         |          8545 |
| `mailpit`   | Local email capture                                    |   1025 / 8025 |
| `push-mock` | Local push notification simulator                      |          8091 |
| `minio`     | Local object-storage simulator                         |   9000 / 9001 |

## Makefile commands

- `bootstrap`: download pinned Go and npm dependencies.
- `generate`: run sqlc generation.
- `fmt` / `fmt-check`: format or verify Go, TypeScript, YAML, Markdown, and Swift sources.
- `lint`: Go vet, ESLint, and TypeScript checks.
- `test`: Go and Web/Admin unit tests.
- `test-integration`: real PostgreSQL/Redis connectivity tests through Compose.
- `test-e2e`: explicit Phase 0 no-product-flow marker; later phases replace it with real flows.
- `migrate-check`: validate reversible migration pairs.
- `openapi-check`: lint the OpenAPI contract.
- `secret-scan`: scan the working tree with pinned gitleaks rules.
- `build`: generate and build Go, Web/Admin, plus iOS on macOS.
- `compose-config`, `compose-up`, `compose-down`: validate and operate the local stack.
- `ci`: aggregate the host-independent checks.

## CI jobs

- `go`: generation drift, formatting, vet, and Go tests.
- `web`: npm lockfile install, formatting, ESLint, typecheck, unit tests, and both Next.js builds.
- `contracts-security`: OpenAPI lint and gitleaks scan.
- `integration-migrations`: real PostgreSQL/Redis integration plus migration up/down/up.
- `ios`: Swift tests and an iOS Simulator build on macOS.
- `containers`: build the Go and Next.js application images and validate Compose.

No workflow merges, tags, publishes releases, or deploys an environment.
