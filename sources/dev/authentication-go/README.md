# auth-service (Go + Gin)

Authentication and authorization microservice using Gin and MySQL. The service
implements OAuth2, JWT access/refresh tokens, pluggable auth providers,
membership tiers, invite codes, teams, and the admin API used by the dashboard.

The storage boundary is `internal/repository`: handlers depend only on the
repository interfaces. The default target backend is MySQL, with the legacy
Azure Table adapter retained for migration and rollback during the cutover.

## Architecture

```text
cmd/auth-service/main.go        entrypoint + seed/migrate subcommands
internal/
  config/        env-based configuration
  domain/        storage-agnostic entity models + value types
  apperror/      typed error model -> HTTP/JSON mapping
  auth/          JWT, password/client-secret hashing, PKCE, OAuth2 helpers
  repository/    storage interfaces
    mysql/       MySQL implementation and schema creation
    aztables/    legacy Azure Table implementation and export helper
    snapshot/    storage-neutral migration payload
  storage/       config-to-repository factory
  handlers/      HTTP handlers
  server/        Gin router wiring
  seed/          admin bootstrap
```

## Build, Test, Run

Start local MySQL:

```bash
docker compose up -d mysql
```

Run checks:

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./... -count=1
```

The integration suite uses MySQL. Override the local test database with:

```bash
TEST_MYSQL_DSN="mysql://auth:auth_password@127.0.0.1:3306/auth_test" go test ./internal/server -v -count=1
```

Run the service locally:

```bash
STORAGE_BACKEND=mysql \
MYSQL_DSN="mysql://auth:auth_password@127.0.0.1:3306/auth" \
go run ./cmd/auth-service
```

Bootstrap the first admin:

```bash
STORAGE_BACKEND=mysql \
MYSQL_DSN="mysql://auth:auth_password@127.0.0.1:3306/auth" \
go run ./cmd/auth-service seed admin@example.com MyPassword1!
```

## Azure Tables To MySQL Migration

Dry-run export from the legacy Azure Tables backend:

```bash
AZURE_STORAGE_CONNECTION_STRING="..." \
go run ./cmd/auth-service migrate-storage azure-to-mysql --dry-run
```

Import into MySQL:

```bash
AZURE_STORAGE_CONNECTION_STRING="..." \
MYSQL_DSN="mysql://user:password@tcp-host:3306/auth" \
go run ./cmd/auth-service migrate-storage azure-to-mysql --clear-target
```

`--clear-target` deletes target MySQL rows before import. Use it only for a
fresh rehearsal or planned cutover window.

## Tencent Cloud MySQL

Production should use `STORAGE_BACKEND=mysql` and a Tencent Cloud MySQL DSN.
Use TLS when required by the instance configuration. The service accepts both
URL style and Go driver style DSNs, for example:

```text
mysql://auth_user:secret@host:3306/auth
auth_user:secret@tcp(host:3306)/auth?tls=true&parseTime=true&loc=UTC
```

The service normalizes DSNs to enable `parseTime=true`, UTC timestamps, and
`utf8mb4` by default.

## Docker

The module uses a local `replace` for the sibling `x` library. Build images from
vendored dependencies:

```bash
go mod vendor
docker build -t auth-service-go .
docker compose up --build
```

## Environment Variables

| Variable | Required | Default |
|----------|----------|---------|
| `STORAGE_BACKEND` | No | `mysql` when `MYSQL_DSN` exists, otherwise `azure_table` |
| `MYSQL_DSN` | When MySQL | - |
| `AZURE_STORAGE_CONNECTION_STRING` | When `azure_table` or migration source | - |
| `JWT_PRIVATE_KEY_PATH` | No | `keys/private.pem` |
| `JWT_PUBLIC_KEY_PATH` | No | `keys/public.pem` |
| `JWT_ISSUER` | No | `auth-service` |
| `JWT_ACCESS_TOKEN_EXPIRY_SECS` | No | `3600` |
| `JWT_REFRESH_TOKEN_EXPIRY_DAYS` | No | `30` |
| `SERVER_HOST` | No | `127.0.0.1` |
| `SERVER_PORT` | No | `3000` |
| `CORS_ALLOWED_ORIGINS` | No | `http://localhost:5173,http://localhost:3000` |
| `AUTH_ENABLE_TEST_PROVIDERS` | No | `false` |
| `STRIDE_REQUIRE_INVITE_CODE` | No | `false` |
| `APP_VERSION` | No | `dev` |
| `LOG_LEVEL` / `LOG_FORMAT` | No | `debug` / `json` |

## API Surface

| Prefix | Auth | Endpoints |
|--------|------|-----------|
| `/oauth/*` | Basic | `token`, `revoke`, `introspect` |
| `/api/auth/*` | `X-Client-Id` | `register`, `login`, `provider/:id/login`, `refresh`, `logout` |
| `/api/users/*` | Bearer | `me`, accounts, teams |
| `/api/teams/*` | Bearer | team CRUD, join/leave/transfer-owner, members |
| `/admin/*` | Bearer admin | app/provider/user/team/invite-code management |
| `/health` | none | health + version |
