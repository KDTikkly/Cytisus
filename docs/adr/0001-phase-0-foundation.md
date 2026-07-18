# ADR 0001: Phase 0 repository foundation

- Status: Accepted for Phase 0
- Date: 2026-07-19

## Context

The repository started empty except for the Cytisus development pack. The PDM locks the backend, database, asynchronous model, client platforms, local runtime, and the simulator-versus-production boundary. Phase 0 must create a runnable baseline without implementing financial product behavior.

## Decision

Use a monorepo with:

- a Go 1.26 modular monolith and separate API, worker, migration, and simulator entry points;
- PostgreSQL accessed through pgx and sqlc, with explicit reversible SQL migrations;
- PostgreSQL transactional outbox reserved for Phase 2, with no Kafka or NATS;
- two independent Next.js 16 applications for customer Web and Admin Web;
- a Swift Package containing the native SwiftUI application skeleton;
- REST contracts defined by OpenAPI;
- Docker Compose for PostgreSQL, Redis, all local applications, Anvil, mail capture, push simulation, and object-storage simulation;
- CI jobs separated by Go, Web, iOS, contracts/security, migrations/integration, and container builds.

The API exposes only operational health in Phase 0. The local simulator exposes a synthetic demo identity and refuses to start when the environment is `production`.

## Consequences

- Product modules can be added behind explicit application, repository, provider, transport, and projection boundaries.
- The local environment is usable without paid providers or real secrets.
- iOS validation requires macOS/Xcode and therefore runs in CI when contributors use another OS.
- Branch protection, provider selection, legal approvals, production credentials, and production deployment remain human-owned.

## Rejected alternatives

- A microservice fleet was rejected because the PDM locks a modular monolith for MVP.
- An ORM and SQLite were rejected because the PDM locks pgx, sqlc, explicit SQL, and PostgreSQL integration testing.
- Kafka/NATS were rejected for MVP because the transactional outbox is locked.
- A cross-platform UI replacement for SwiftUI was rejected because native iOS is locked.
