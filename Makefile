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

.DEFAULT_GOAL := help

.PHONY: help setup generate proto sqlc build build-embed web web-check dev seed test lint fmt tidy migrate

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

build: ## Build all Go binaries
	go build ./...

# Run pnpm from inside apps/web (not `pnpm --dir`) so corepack resolves the pinned
# pnpm@9.15.0 from apps/web/package.json. `--dir` keeps the shell CWD at the repo root,
# where corepack falls back to its default major and mismatches a 9.x-built node_modules.
web: ## Build the dashboard
	cd apps/web && pnpm build

web-check: ## Lint and typecheck the dashboard (mirrors CI's web steps)
	cd apps/web && pnpm lint
	cd apps/web && pnpm typecheck

build-embed: web ## Build the single binary with the dashboard embedded (bin/controlplane)
	rm -rf internal/platform/web/dist
	cp -r apps/web/dist internal/platform/web/dist
	mkdir -p bin
	go build -tags embed_web -o bin/controlplane ./cmd/controlplane

dev: ## Run the control plane in dev mode (expects `pnpm --dir apps/web dev` in another terminal)
	PLORIGO_ENV=dev PLORIGO_PUBLIC_URL=http://localhost:8080 go run ./cmd/controlplane

seed: ## Create/refresh a LOCAL dev login user (dev only). Override SEED_EMAIL / SEED_PASSWORD.
	PLORIGO_ENV=dev APP_MASTER_KEY="$(APP_MASTER_KEY)" DATABASE_URL="$(DATABASE_URL)" \
		PLORIGO_SEED_EMAIL="$(SEED_EMAIL)" PLORIGO_SEED_PASSWORD="$(SEED_PASSWORD)" \
		go run ./cmd/seed

test: ## Run Go tests
	go test ./...

lint: ## Run golangci-lint (incl. depguard module-boundary rules) and buf lint
	go tool golangci-lint run
	cd proto && go tool buf lint

fmt: ## Format Go code
	go tool golangci-lint fmt

tidy: ## Tidy go.mod
	go mod tidy

migrate: ## Apply database migrations (uses $$DATABASE_URL)
	go tool goose -dir migrations postgres "$(DATABASE_URL)" up
