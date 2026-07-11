# CLAUDE.md

Guidance for Claude Code when working in the Go auth service
(`sources/dev/authentication-go/`).

## Project Overview

Go + Gin authentication/authorization microservice. It preserves the production
API and storage contracts: same endpoints, same JSON shapes, same JWT (RS256)
claims, and drop-in data compatibility with Azure Table Storage (same table
names, PK/RK schemes, index rows).

## Build, Test, Lint

```bash
go build ./...                                   # build
go vet ./...                                     # static analysis
gofmt -l .                                       # format check (empty = clean)
gofmt -w .                                       # auto-format
go test ./...                                    # all tests (server suite needs Azurite)
go test ./internal/auth/                         # pure unit tests, no Azurite
go test ./internal/server/ -run TestX -v         # a single integration test
```

Integration tests (`internal/server/integration_test.go`) require Azurite on
port 10002; they `t.Skip` if it is unreachable. Override the endpoint with
`TEST_STORAGE_CONNECTION_STRING`. Each test calls `newTestApp`, which clears and
recreates all tables, then bootstraps an admin via `seed.Bootstrap`.

## Architecture

Layered with a swappable storage adapter (see README for the full tree):

- `internal/domain` — entity models + value types (`MembershipTier`,
  `InviteCodeKind`), storage- and transport-agnostic.
- `internal/repository` — **the adapter boundary**: interfaces only. Handlers
  depend on these, never on a concrete store.
- `internal/repository/aztables` — Azure Table Storage implementation. Each
  sub-repository (`userRepo`, `appRepo`, …) wraps one `*aztables.Client`; the
  composite `Repository` returns them. Secondary lookups use index rows; invite
  codes are consumed atomically via ETag (`If-Match`).
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
- **Datetimes:** stored as `2006-01-02T15:04:05.000000` (`fmtDT` in the aztables
  adapter); API responses use `displayDT` in `handlers` to preserve the existing
  display contract (space separator, 0/3/6/9 fractional digits).
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
