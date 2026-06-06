# Plorigo — developer tasks. Run `make help` for the list.
# Dev tools (buf, sqlc, goose, golangci-lint) are pinned in go.mod and run via `go tool`.

SHELL := /bin/bash
DATABASE_URL ?= postgres://plorigo:plorigo@localhost:5432/plorigo?sslmode=disable

.DEFAULT_GOAL := help

.PHONY: help setup generate proto sqlc build build-embed web dev test lint fmt tidy migrate

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

setup: ## Install toolchain and dependencies
	corepack enable pnpm
	go mod download
	pnpm --dir apps/web install

generate: proto sqlc ## Generate all code (proto + sqlc)

proto: ## Generate Go + TS clients from protobuf
	cd proto && go tool buf generate

sqlc: ## Generate the type-safe DB package
	go tool sqlc generate

build: ## Build all Go binaries
	go build ./...

web: ## Build the dashboard
	pnpm --dir apps/web build

build-embed: web ## Build the single binary with the dashboard embedded (bin/controlplane)
	rm -rf internal/platform/web/dist
	cp -r apps/web/dist internal/platform/web/dist
	mkdir -p bin
	go build -tags embed_web -o bin/controlplane ./cmd/controlplane

dev: ## Run the control plane in dev mode (expects `pnpm --dir apps/web dev` in another terminal)
	PLORIGO_ENV=dev go run ./cmd/controlplane

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
