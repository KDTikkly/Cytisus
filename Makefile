SHELL := /usr/bin/env bash

COMPOSE := docker compose --env-file .env.example -f deploy/docker-compose.yml
POSTGRES_HOST_PORT ?= 5432
TEST_DATABASE_URL ?= postgres://cytisus:local_only_cytisus@localhost:$(POSTGRES_HOST_PORT)/cytisus?sslmode=disable
GO_SOURCES := $(shell find apps internal pkg tests tools -name '*.go' -type f 2>/dev/null)
GO_PACKAGES := ./apps/... ./internal/... ./tools/...

.PHONY: bootstrap generate fmt fmt-check lint test test-integration test-e2e migrate-check openapi-check secret-scan build build-ios compose-config compose-up compose-down ci

bootstrap:
	go mod download
	npm ci

generate:
	docker run --rm -v "$(CURDIR):/src" -w /src sqlc/sqlc:1.31.1 generate -f db/sqlc.yaml

fmt:
	gofmt -w $(GO_SOURCES)
	npm run format

fmt-check:
	test -z "$$(gofmt -l $(GO_SOURCES))"
	npm run format:check

lint:
	go vet $(GO_PACKAGES)
	go run ./tools/financecheck
	npm run lint
	npm run typecheck

test:
	go test $(GO_PACKAGES)
	npm test

test-integration:
	POSTGRES_HOST_PORT='$(POSTGRES_HOST_PORT)' $(COMPOSE) up -d --wait postgres redis
	DATABASE_URL='$(TEST_DATABASE_URL)' REDIS_ADDR='localhost:6379' go test -tags=integration ./tests/integration

test-e2e:
	POSTGRES_HOST_PORT='$(POSTGRES_HOST_PORT)' $(COMPOSE) up -d --wait postgres redis
	DATABASE_URL='$(TEST_DATABASE_URL)' REDIS_ADDR='localhost:6379' go test -tags=integration -run '^TestPaperAPIEndToEnd$$' ./tests/integration

migrate-check:
	go run ./tools/migratecheck

openapi-check:
	npm run openapi:check

secret-scan:
	docker run --rm -v "$(CURDIR):/src" -w /src zricethezav/gitleaks:v8.30.1 dir --redact --no-banner .

build: generate
	go build ./apps/...
	npm run build
	$(MAKE) build-ios

build-ios:
	@if [[ "$$(uname -s)" == 'Darwin' ]]; then \
		cd apps/ios && xcodebuild -scheme Cytisus -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build; \
	else \
		echo 'iOS build requires macOS/Xcode; enforced by the macOS CI job.'; \
	fi

compose-config:
	$(COMPOSE) config --quiet

compose-up:
	$(COMPOSE) up --build --wait

compose-down:
	$(COMPOSE) down --remove-orphans

ci: generate fmt-check lint test migrate-check openapi-check secret-scan build
