# Cytisus

Cytisus is a simulation-first foundation for a regulated-finance product. Phase 0 establishes the monorepo, local runtime, contracts, CI, security checks, and application skeletons without activating real financial providers or implementing product flows.

## Prerequisites

- Go 1.26+
- Node.js 24+ and npm 11+
- Docker with Compose v2
- GNU Make
- macOS with current Xcode for the iOS build

## Start locally

```bash
cp .env.example .env
make bootstrap
make compose-up
```

Then open:

- API health: `http://localhost:8080/healthz`
- Local simulator: `http://localhost:8090/healthz`
- Web: `http://localhost:3000`
- Admin: `http://localhost:3001`
- Mail catcher: `http://localhost:8025`
- Object storage: `http://localhost:9001`

All local identities and credentials are synthetic and explicitly marked as simulated. Production mode rejects simulator startup.

## Quality gates

```bash
make generate
make fmt
make lint
make test
make test-integration
make test-e2e
make migrate-check
make openapi-check
make secret-scan
make build
```

The iOS build runs on the macOS CI job when the local host is not macOS. See `docs/repository-plan.md` for the Phase 0 architecture and `docs/human-approvals.md` for decisions that remain human-owned.

## Product specifications

- `PDM.md`
- `AGENTS.md`
- `DECISION_REGISTER.md`
- `CODEX_TASKS.md`
- `CODEX_CHAT_BOOTSTRAP.md`
- `pdm_manifest.json`

Never merge `main` automatically, deploy production, upload real customer data, or present a simulator as a real financial service.
