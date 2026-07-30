# CLAUDE.md

Guidance for Claude Code when working in the Go auth service
(`sources/dev/authentication-go/`).

## Project Overview

Go + Gin auth microservice. The runtime storage target is MySQL; the legacy
Azure Table adapter is retained only for migration and rollback during cutover.

## Build, Test, Lint

```bash
go build ./...                                   # build
go vet ./...                                     # static analysis
gofmt -l .                                       # format check (empty = clean)
gofmt -w .                                       # auto-format
go test ./...                                    # all tests (server suite needs MySQL)
go test ./internal/auth/                         # pure unit tests, no MySQL
go test ./internal/server/ -run TestX -v         # a single integration test
make swagger                                     # regenerate cmd/auth-service/docs
go build -tags swagger ./...                     # build incl. the Swagger UI
```

A `Makefile` wraps these (`make build|vet|fmt|test|swagger|build-service-swagger`).

Integration tests (`internal/server/integration_test.go`) require MySQL on
`127.0.0.1:3306` by default; they `t.Skip` if it is unreachable. Start it with
`docker compose up -d mysql`. Override the endpoint with `TEST_MYSQL_DSN`. Each
test calls `newTestApp`, which clears all MySQL tables, then bootstraps an admin
via `seed.Bootstrap`.

## CLI (cobra)

`cmd/auth-service` is a single `github.com/spf13/cobra` binary. `main.go` is a
thin root that wires subcommands, each in its own `cmd_*.go` file: `serve` (HTTP
server) and `seed`. The container default command is `serve`; maintenance tasks
run by overriding it. Keep subcommands thin — config load + dependency wiring
only; all logic lives in `internal/`.

## Swagger

Endpoints carry [swaggo/swag](https://github.com/swaggo/swag) annotations; the
general API info (title, `securityDefinitions`: `ClientID`, `BearerAuth`,
`BasicAuth`) lives on `cmd/auth-service/main.go` (the `swag init -g` entry file).
The generated `cmd/auth-service/docs` package is committed. The UI mount
(`internal/server/swagger.go`) is behind the `swagger` build tag with a no-op
stub (`swagger_stub.go`) for the default build, so plain `go build`/`go test`
never need the generated code. `server.NewRouter` calls `mountSwagger` and the
UI is served only when `SWAGGER_ENABLED=true`. After changing routes/DTOs or
their annotations, rerun `make swagger` and commit the regenerated docs.

## Architecture

Layered with a swappable storage adapter (see README for the full tree):

- `internal/domain` — entity models + value types (`MembershipTier`,
  `InviteCodeKind`), storage- and transport-agnostic.
- `internal/repository` — **the adapter boundary**: interfaces only. Handlers
  depend on these, never on a concrete store.
- `internal/repository/mysql` — MySQL implementation. Unique indexes replace
  Azure Table secondary-index rows; invite codes are consumed atomically with a
  conditional `UPDATE ... WHERE used_at IS NULL`.
- `internal/repository/aztables` — legacy Azure Table implementation plus
  `ExportSnapshot` (storage-neutral snapshot export helper).
- `internal/auth` — JWT issue/verify (custom claims so `aud` stays a single
  string and `membership` is a snake_case string), argon2id passwords,
  SHA-256 client secrets (with legacy argon2 fallback), PKCE, OAuth2 helpers.
- `internal/middleware` — Gin auth context helpers, the per-IP sliding-window
  rate limiter, CORS, and `RespondError`.
- `internal/handlers` — one `*Handler` with methods per endpoint; reads auth
  context via the `middleware` getters.
- `internal/server` — router wiring and rate-limiter groups.

## Key Conventions

- **Errors:** return `*apperror.Error` (typed, with HTTP status + stable `error`
  code). Handlers call `middleware.RespondError(c, err)`. Never leak DB detail —
  `apperror.Database` maps to a generic 500.
- **Datetimes:** store UTC timestamps in MySQL `DATETIME(6)`. API responses use
  `displayDT` in `handlers`.
- **Nullable JSON:** fields that are part of the API contract and may be absent
  should usually be Go pointers without `omitempty`, so they serialize as
  `null`. Fields intentionally omitted when absent should use `omitempty`.
- **Membership / invite-kind** are string-typed enums; parse leniently from
  storage (unknown → default) via `domain.MembershipFromString` /
  `InviteKindFromString`.
- The `/api/users` and `/api/teams` groups intentionally **share** one rate
  limiter instance.
- The `test` provider is gated by `AUTH_ENABLE_TEST_PROVIDERS`; tests enable it
  via config.

## Dependencies

`go.mod` uses a local `replace github.com/zhaochy1990/x => ../../../../x` for the
shared `x` library. Docker builds use `go mod vendor` to capture it — and the
swaggo packages needed by the `-tags swagger` image build — into vendor/
(gitignored; run `go mod vendor` before `docker build`).
