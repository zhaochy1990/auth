# auth-service (Go + Gin)

A Go rewrite of the Rust authentication/authorization microservice, using
[Gin](https://gin-gonic.com/) and Azure Table Storage. It is a faithful port:
same endpoints, same JSON request/response shapes, same JWT (RS256) claims, and
**drop-in data compatibility** with the existing deployment (identical table
names, partition/row-key schemes, and secondary index rows).

Implements OAuth2 (authorization-code with PKCE, client-credentials, password,
and refresh-token grants), JWT access/refresh tokens, pluggable auth providers,
a membership-tier system, invite codes, teams, and an admin API.

## Architecture

Layered, with a **swappable storage adapter** at the core:

```
cmd/auth-service/main.go        entrypoint + seed/migrate subcommands
internal/
  config/        env-based configuration
  domain/        storage-agnostic entity models + value types
  apperror/      typed error model → HTTP/JSON mapping
  auth/          JWT (RS256), password/argon2, client-secret, PKCE, OAuth2 helpers
    providers/   pluggable providers (wechat, test) + factory
  repository/    storage interfaces  ← the adapter boundary
    aztables/    Azure Table Storage implementation
  middleware/    Gin auth extractors, rate limiter, CORS, error responder
  handlers/      HTTP handlers (business logic)
  server/        Gin router wiring (route groups, limiters)
  seed/          admin bootstrap
```

Handlers depend only on the `repository` interfaces — never on a concrete
store — so the backend can be swapped (Postgres, SQLite, in-memory, …) by
implementing `repository.Repository`, with **zero changes to business logic**.
The Azure Tables adapter lives entirely in `internal/repository/aztables`.

Shared utilities (`zap` logging) come from `github.com/zhaochy1990/x`.

## Prerequisites

- Go 1.25+
- [Azurite](https://learn.microsoft.com/azure/storage/common/storage-use-azurite)
  (Azure Storage emulator) on port 10002 for local dev/tests, or a real Azure
  Storage account.

Start Azurite via Docker:

```bash
docker compose up azurite          # or: docker run -p 10002:10002 mcr.microsoft.com/azure-storage/azurite azurite-table --tableHost 0.0.0.0
```

## Build, test, run

```bash
go build ./...                     # build everything
go vet ./...                       # static checks
gofmt -l .                         # formatting check (empty = clean)

# Tests. The internal/server suite needs Azurite on :10002.
go test ./...                      # all tests
go test ./internal/auth/           # pure unit tests (no Azurite)
go test ./internal/server/ -v      # integration tests

# Run the server
AZURE_STORAGE_CONNECTION_STRING="UseDevelopmentStorage=true" go run ./cmd/auth-service

# Bootstrap the first admin (creates the Admin Dashboard app + admin user)
AZURE_STORAGE_CONNECTION_STRING="..." go run ./cmd/auth-service seed admin@example.com MyPassword1!

# Idempotent backfill migrations (invite-code kinds, user invite-code linkage)
AZURE_STORAGE_CONNECTION_STRING="..." go run ./cmd/auth-service migrate
```

Override the test connection string with `TEST_STORAGE_CONNECTION_STRING`.

## Docker

The module uses a local `replace` for the sibling `x` library, so the image is
built from **vendored** dependencies (the build never reaches outside the build
context):

```bash
go mod vendor
docker build -t auth-service-go .
docker compose up --build          # azurite + auth on :3001
```

## Environment variables

| Variable | Required | Default |
|----------|----------|---------|
| `AZURE_STORAGE_CONNECTION_STRING` | Yes | – |
| `JWT_PRIVATE_KEY_PATH` | No | `keys/private.pem` |
| `JWT_PUBLIC_KEY_PATH` | No | `keys/public.pem` |
| `JWT_ISSUER` | No | `auth-service` |
| `JWT_ACCESS_TOKEN_EXPIRY_SECS` | No | `3600` |
| `JWT_REFRESH_TOKEN_EXPIRY_DAYS` | No | `30` |
| `SERVER_HOST` | No | `127.0.0.1` |
| `SERVER_PORT` | No | `3000` |
| `CORS_ALLOWED_ORIGINS` | No | `http://localhost:5173,http://localhost:3000` |
| `AUTH_ENABLE_TEST_PROVIDERS` | No | `false` |
| `STRIDE_REQUIRE_INVITE_CODE` | No | `false` (read per-request) |
| `APP_VERSION` | No | `dev` (surfaced at `/health`) |
| `LOG_LEVEL` / `LOG_FORMAT` | No | `debug` / `json` |

## API surface

| Prefix | Auth | Endpoints |
|--------|------|-----------|
| `/oauth/*` | Basic (client_id:secret) | `token`, `revoke`, `introspect` |
| `/api/auth/*` | `X-Client-Id` (logout: Bearer) | `register`, `login`, `provider/:id/login`, `refresh`, `logout` |
| `/api/users/*` | Bearer | `me` (GET/PATCH/DELETE), `me/accounts`, account link/unlink, `me/teams` |
| `/api/teams/*` | Bearer | team CRUD, join/leave/transfer-owner, members |
| `/admin/*` | Bearer (admin role) | application/provider/user CRUD, stats, invite codes, team management |
| `/health` | none | health + version |
