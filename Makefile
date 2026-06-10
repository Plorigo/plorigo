# Plorigo — developer tasks. Run `make help` for the list.
# Dev tools (buf, sqlc, goose, golangci-lint) are pinned in go.mod and run via `go tool`.

SHELL := /bin/bash

# In a Conductor workspace each clone runs its own Postgres on CONDUCTOR_PORT+2
# (see scripts/conductor-env.sh); outside Conductor, use the standard 5432.
ifdef CONDUCTOR_PORT
PG_PORT := $(shell echo $$(( $(CONDUCTOR_PORT) + 2 )))
else
PG_PORT := 5432
endif
DATABASE_URL ?= postgres://plorigo:plorigo@localhost:$(PG_PORT)/plorigo?sslmode=disable

# Throwaway local dev key (base64 of a 32-byte string), matching scripts/conductor-env.sh.
# Real deployments set APP_MASTER_KEY in the environment, which overrides this default;
# it only lets the dev/seed targets run standalone.
APP_MASTER_KEY ?= cGxvcmlnby1kZXYtb25seS1ub3QtYS1zZWNyZXQtMzI=

# Credentials for the local dev login created by `make seed`. Override on the CLI:
#   make seed SEED_EMAIL=you@example.com SEED_PASSWORD=secret123
SEED_EMAIL ?= dev@plorigo.local
SEED_PASSWORD ?= devpassword

# Output path for the linux agent binary the e2e harness installs into a container.
E2E_AGENT_BIN ?= dist/plorigo-agent

.DEFAULT_GOAL := help

.PHONY: help setup generate proto sqlc check-generated verify build build-embed web web-check dev seed test lint fmt tidy migrate e2e-agent

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

setup: ## Install toolchain and dependencies
	corepack enable pnpm
	go mod download
	cd apps/web && pnpm install --config.confirm-modules-purge=false

generate: proto sqlc ## Generate all code (proto + sqlc)

proto: ## Generate Go + TS clients from protobuf
	cd proto && go tool buf generate

sqlc: ## Generate the type-safe DB package
	go tool sqlc generate

# proto/gen (Go) and apps/web/src/gen (TS) are git-ignored and only exist after
# `make generate`. Without them, `go build`/`tsc` fail with a confusing "package not
# found" (the Go loader even suggests `go get`, which won't help). Fail fast with the
# real fix instead. The committed sqlc output is always present, so it isn't checked here.
check-generated: ## Verify generated clients exist (run `make generate` if this fails)
	@if [ ! -d proto/gen ] || [ -z "$$(ls -A proto/gen 2>/dev/null)" ] || \
	    [ ! -d apps/web/src/gen ] || [ -z "$$(ls -A apps/web/src/gen 2>/dev/null)" ]; then \
		echo "error: generated clients are missing (proto/gen and/or apps/web/src/gen)." >&2; \
		echo "       These are git-ignored and generated from the .proto sources." >&2; \
		echo "       Run 'make generate' first. See docs/development.md." >&2; \
		exit 1; \
	fi

# Run the same checks CI runs, in the same order, with one command. Regenerates first
# so a stale or missing client never trips you up mid-loop.
verify: generate build test lint web-check ## Run the full CI-order check loop (recommended before pushing)

build: check-generated ## Build all Go binaries
	go build ./...

# Run pnpm from inside apps/web (not `pnpm --dir`) so corepack resolves the pinned
# pnpm@9.15.0 from apps/web/package.json. `--dir` keeps the shell CWD at the repo root,
# where corepack falls back to its default major and mismatches a 9.x-built node_modules.
web: check-generated ## Build the dashboard
	cd apps/web && pnpm build

web-check: check-generated ## Lint and typecheck the dashboard (mirrors CI's web steps)
	cd apps/web && pnpm lint
	cd apps/web && pnpm typecheck

build-embed: web ## Build the single binary with the dashboard embedded (bin/controlplane)
	rm -rf internal/platform/web/dist
	cp -r apps/web/dist internal/platform/web/dist
	mkdir -p bin
	go build -tags embed_web -o bin/controlplane ./cmd/controlplane

dev: check-generated ## Run the control plane in dev mode (expects `pnpm --dir apps/web dev` in another terminal)
	PLORIGO_ENV=dev PLORIGO_PUBLIC_URL=http://localhost:8080 go run ./cmd/controlplane

seed: ## Create/refresh a LOCAL dev login user (dev only). Override SEED_EMAIL / SEED_PASSWORD.
	PLORIGO_ENV=dev APP_MASTER_KEY="$(APP_MASTER_KEY)" DATABASE_URL="$(DATABASE_URL)" \
		PLORIGO_SEED_EMAIL="$(SEED_EMAIL)" PLORIGO_SEED_PASSWORD="$(SEED_PASSWORD)" \
		go run ./cmd/seed

test: check-generated ## Run Go tests
	go test ./...

lint: check-generated ## Run golangci-lint (incl. depguard module-boundary rules) and buf lint
	go tool golangci-lint run
	cd proto && go tool buf lint

fmt: ## Format Go code
	go tool golangci-lint fmt

tidy: ## Tidy go.mod
	go mod tidy

migrate: ## Apply database migrations (uses $$DATABASE_URL)
	go tool goose -dir migrations postgres "$(DATABASE_URL)" up

# Agent install end-to-end: builds a linux agent binary, then runs the real installer +
# agent in a clean ubuntu container against an in-process control plane, proving register
# AND resume. Needs Docker and a running, migrated Postgres. Local-only (not in CI); the
# fuller fresh-Ubuntu preparation lands later (see ROADMAP.md). See docs/development.md.
e2e-agent: check-generated migrate ## Run the agent install e2e on a real container (Docker + Postgres; not in CI)
	GOOS=linux GOARCH=$$(go env GOARCH) CGO_ENABLED=0 go build -o $(E2E_AGENT_BIN) ./cmd/agent
	APP_MASTER_KEY="$(APP_MASTER_KEY)" DATABASE_URL="$(DATABASE_URL)" \
		PLORIGO_E2E_AGENT_BIN="$(CURDIR)/$(E2E_AGENT_BIN)" \
		go test -tags e2e -run TestE2EAgentInstall -count=1 -v ./internal/app/...
