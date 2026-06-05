# syntax=docker/dockerfile:1
#
# Multi-stage build for the production single binary (dashboard embedded).
# Run `make generate` before building so proto/gen and apps/web/src/gen are present
# in the build context (they are git-ignored generated code).

# --- Stage 1: build the dashboard ---
FROM node:24-alpine AS web
RUN corepack enable
WORKDIR /app
COPY pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/
RUN pnpm --dir apps/web install --no-frozen-lockfile
COPY apps/web apps/web
RUN pnpm --dir apps/web build

# --- Stage 2: build the Go binary with the embedded dashboard ---
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/apps/web/dist ./internal/platform/web/dist
RUN CGO_ENABLED=0 go build -tags embed_web -o /out/controlplane ./cmd/controlplane

# --- Stage 3: minimal runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/controlplane /controlplane
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/controlplane"]
