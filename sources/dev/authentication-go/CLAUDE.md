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
```

Integration tests (`internal/server/integration_test.go`) require MySQL on
`127.0.0.1:3306` by default; they `t.Skip` if it is unreachable. Start it with
`docker compose up -d mysql`. Override the endpoint with `TEST_MYSQL_DSN`. Each
test calls `newTestApp`, which clears all MySQL tables, then bootstraps an admin
via `seed.Bootstrap`.

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
  `ExportSnapshot`, used by `migrate-storage azure-to-mysql`.
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
shared `x` library. Docker builds use `go mod vendor` to capture it (vendor/ is
gitignored — run `go mod vendor` before `docker build`).
