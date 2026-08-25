# auth-service (Go + Gin)

Authentication and authorization microservice using Gin and MySQL. The service
implements OAuth2, JWT access/refresh tokens, pluggable auth providers,
membership tiers, invite codes, teams, and the admin API used by the dashboard.

The storage boundary is `internal/repository`: handlers depend only on the
repository interfaces. The default target backend is MySQL, with the legacy
Azure Table adapter retained for migration and rollback during the cutover.
Short-lived SMS verification codes live in Redis (`internal/repository/redis`),
which the SMS login endpoints fail closed on (503) rather than falling back to
another store.

## Architecture

```text
cmd/auth-service/               unified cobra CLI (one binary, subcommands)
  main.go                       root command + Swagger general API info
  cmd_serve.go                  `serve`  — start the Gin HTTP server
  cmd_seed.go                   `seed`   — bootstrap the admin user + app client
  docs/                         generated OpenAPI/Swagger docs (`make swagger`)
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

## CLI

The service is one binary with cobra subcommands (`auth-service <command>`):

| Command | Purpose |
|---------|---------|
| `serve` | Start the Gin HTTP server (default container command) |
| `seed [email] [password]` | Bootstrap the admin user and Admin Dashboard app client |

Run `auth-service <command> --help` for flags.

## Build, Test, Run

Start local MySQL and Redis (SMS verification codes):

```bash
docker compose up -d mysql redis
```

Run checks (a `Makefile` wraps these):

```bash
make build          # go build ./...
make vet            # go vet ./...
make fmt-check      # gofmt -l .
make test           # go test ./...
make swagger        # regenerate cmd/auth-service/docs from swag annotations
```

The integration suite uses MySQL. Override the local test database with:

```bash
TEST_MYSQL_DSN="mysql://auth:auth_password@127.0.0.1:3306/auth_test" go test ./internal/server -v -count=1
```

Run the service locally:

```bash
STORAGE_BACKEND=mysql \
MYSQL_DSN="mysql://auth:auth_password@127.0.0.1:3306/auth" \
go run ./cmd/auth-service serve
```

Bootstrap the first admin:

```bash
STORAGE_BACKEND=mysql \
MYSQL_DSN="mysql://auth:auth_password@127.0.0.1:3306/auth" \
go run ./cmd/auth-service seed admin@example.com MyPassword1!
```

## Swagger / OpenAPI

Endpoints are annotated with [swaggo/swag](https://github.com/swaggo/swag). The
generated spec lives in `cmd/auth-service/docs` (committed; regenerate with
`make swagger`). The Swagger UI is compiled in only with the `swagger` build tag
and served at `/swagger/index.html` when `SWAGGER_ENABLED=true`:

```bash
make build-service-swagger      # go build -tags swagger ...
SWAGGER_ENABLED=true \
STORAGE_BACKEND=mysql MYSQL_DSN="mysql://auth:auth_password@127.0.0.1:3306/auth" \
./bin/auth-service serve
# open http://127.0.0.1:3000/swagger/index.html
```

Plain `go build` / `go test` never need the generated package (a build-tagged
no-op stub replaces the UI), so the default build stays lean.


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

If Tencent Cloud requires a custom CA, set either `MYSQL_TLS_CA_PEM` to the PEM
contents or `MYSQL_TLS_CA_PATH` to a local PEM file. When either is present, the
service registers a named MySQL TLS configuration and uses it in the normalized
DSN, so certificate verification stays enabled.

Deployment expects the production GitHub Environment secret `MYSQL_DSN` to be set
before the release tag is deployed. If the Tencent instance requires a custom CA,
also set `MYSQL_TLS_CA_PEM`. The deploy workflow stores these as Azure Container
Apps secrets and sets:

```text
STORAGE_BACKEND=mysql
MYSQL_DSN=secretref:mysql-dsn
MYSQL_TLS_CA_PEM=secretref:mysql-tls-ca-pem  # only when configured
```

Cutover checklist:

1. Create the Tencent Cloud MySQL database and application user.
2. Confirm network access from the migration runner and Azure Container Apps.
3. Run a dry-run export from Azure Tables and record the exported counts.
4. Rehearse the import against local MySQL with `--clear-target`.
5. Set the production GitHub Environment secret `MYSQL_DSN` to the Tencent DSN,
   and `MYSQL_TLS_CA_PEM` if the Tencent instance requires a custom CA.
6. During the cutover window, run the import against Tencent MySQL.
7. Confirm exported and imported counts match in the migration output.
8. Deploy the release and verify `/health`, admin login, and admin list APIs.
9. Keep the Azure Table connection available only for rollback until the cutover
   is accepted.

## Docker

The module uses a local `replace` for the sibling `x` library. Build images from
vendored dependencies (the image is built with `-tags swagger`, so the UI is
available at runtime when `SWAGGER_ENABLED=true`):

```bash
go mod vendor
docker build -t auth-service-go .
docker compose up --build
```

The image bundles every subcommand; the default command is `serve`. Override it
to run maintenance tasks, e.g. `docker run auth-service-go seed admin@example.com`.

## Environment Variables

| Variable | Required | Default |
|----------|----------|---------|
| `STORAGE_BACKEND` | No | `mysql` when `MYSQL_DSN` exists, otherwise `azure_table` |
| `MYSQL_DSN` | When MySQL | - |
| `MYSQL_TLS_CA_PEM` | No | - |
| `MYSQL_TLS_CA_PATH` | No | - |
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
| `SWAGGER_ENABLED` | No | `false` (UI also requires the `swagger` build tag) |
| `STRIDE_REQUIRE_INVITE_CODE` | No | `false` |
| `AUTH_REQUIRE_INVITE_CODE` | No | `false` (alias for the above) |
| `REDIS_ADDR` | No | `127.0.0.1:6379` |
| `REDIS_PASSWORD` | No | - |
| `REDIS_DB` | No | `0` |
| `AUTH_SMS_TEST_MODE` | No | `false` (fixes the code at `123456`, skips Tencent) |
| `TENCENT_SMS_SECRET_ID` | No | - |
| `TENCENT_SMS_SECRET_KEY` | No | - |
| `TENCENT_SMS_SDK_APP_ID` | No | - |
| `TENCENT_SMS_SIGN_NAME` | No | - |
| `TENCENT_SMS_TEMPLATE_ID` | No | - |
| `TENCENT_SMS_REGION` | No | `ap-guangzhou` |
| `SMS_SEND_RATE_LIMIT` | No | `10` (per-IP sends per hour) |
| `SMS_VERIFY_RATE_LIMIT` | No | `60` (per-IP verifies per hour) |
| `WECHAT_CODE2SESSION_URL` | No | WeChat's public `jscode2session` endpoint (tests) |
| `APP_VERSION` | No | `dev` |
| `LOG_LEVEL` / `LOG_FORMAT` | No | `debug` / `json` |

## API Surface

| Prefix | Auth | Endpoints |
|--------|------|-----------|
| `/oauth/*` | Basic | `token` (authz-code, client-creds, refresh, password, `token_exchange`), `revoke`, `introspect` |
| `/api/auth/*` | `X-Client-Id` | `register`, `login`, `provider/:id/login`, `refresh`, `logout`, `sms/send`, `sms/verify` |
| `/api/users/*` | Bearer | `me`, accounts, teams |
| `/api/teams/*` | Bearer | team CRUD, join/leave/transfer-owner, members |
| `/admin/*` | Bearer admin | app/provider/user/team/invite-code management |
| `/health` | none | health + version |
| `/swagger/*` | none | Swagger UI (when `SWAGGER_ENABLED=true` + `swagger` build tag) |

### WeChat mini-program login (OAuth2 token exchange)

WeChat credentials are configured per application via the admin API
(`wechat_app_id` / `wechat_app_secret` on `POST|PATCH /admin/applications`),
stored on the `Application` row — they are **not** read from environment
variables.

Login and binding go through the standard `POST /oauth/token` endpoint as an
RFC 8693 `token_exchange` grant; the endpoint accepts both
`application/x-www-form-urlencoded` (standard) and `application/json` bodies.
A public client identifies itself with `client_id` in the body; confidential
callers may instead use HTTP Basic (client_id:client_secret).

**Login** (identity already bound):

```
POST /oauth/token
Content-Type: application/x-www-form-urlencoded

grant_type=token_exchange
&client_id=my_client_id
&subject_token=<wx.login() code>
&subject_token_type=wechat_mini_program
```

- Bound identity → `200` with the standard token response (`access_token`,
  `refresh_token`, `token_type`, `expires_in`, `scope`); fetch the user via
  `GET /api/users/me`.
- Unbound identity → `400 {"error":"wechat_needs_binding"}` — present the bind flow.
- Invalid/expired code or WeChat API failure → `400` with a mapped error code
  (`wechat_invalid_code`, `wechat_api_error`, …).
- App without WeChat config → `400 wechat_not_configured`.

**Bind** (identity not yet bound) — same grant plus the account credentials; a
successful bind also logs the user in:

```
POST /oauth/token
Content-Type: application/x-www-form-urlencoded

grant_type=token_exchange
&client_id=my_client_id
&subject_token=<wx.login() code>
&subject_token_type=wechat_mini_program
&email=user@example.com
&password=...
```

- Wrong email/password → `401 invalid_credentials`, nothing is bound.
- Identity already bound to another account → `409 wechat_already_bound`.
- Account already bound to a different WeChat identity in this mini-program →
  `409 wechat_already_bound` (changing a binding is not supported yet).

WeChat identities are stored per mini-program in the `auth_user_wechat_links`
table (see `docs/adr/0002-user-wechat-links-table.md`); `users` carries only a
`wechat_bound` flag derived from it.

### SMS verification-code login (mainland-China phone numbers)

SMS login is **login-or-register**: users enter an 11-digit mainland-China
mobile number (starting `1[3-9]`), receive a one-time 6-digit code via Tencent
Cloud SMS, and the first successful verification auto-creates the account;
later verifications log the existing account in.

```
POST /api/auth/sms/send    body: {"phone":"13812345678"}      → 200 {"status":"ok"}
POST /api/auth/sms/verify  body: {"phone":"...","code":"...","invite_code":"..."?}
                            → 200 access_token / refresh_token / token_type / expires_in
```

Both endpoints require the `X-Client-Id` header like the other `/api/auth/*`
routes, and each has a dedicated per-IP rate limiter (send 10/hour, verify
60/hour; override with `SMS_SEND_RATE_LIMIT` / `SMS_VERIFY_RATE_LIMIT`).

- Codes are single-use, expire after 5 minutes, allow 5 verification attempts,
  and sends are throttled to one per 60 seconds and 10 per phone per day.
- Codes live in Redis (SHA-256 hashed, never in plaintext). A Redis outage makes
  send/verify return `503 service_unavailable` — the service fails closed and
  never falls back to another store.
- Missing `TENCENT_SMS_*` env vars do not prevent startup; `send` returns
  `400 sms_not_configured`. Set `AUTH_SMS_TEST_MODE=true` to run the whole flow
  with the fixed code `123456` and no Tencent call (demos / CI).
- When `STRIDE_REQUIRE_INVITE_CODE` (alias `AUTH_REQUIRE_INVITE_CODE`) is on,
  a **new** phone must present a valid `invite_code` in the verify request
  (reusing the existing single-use consumption and membership/user-type
  grants); the invite is never required for existing accounts.
- Error codes: `sms_code_invalid`, `sms_code_expired`, `sms_attempts_exceeded`,
  `sms_send_cooldown`, `sms_daily_limit`, `sms_not_configured`.
- Phone-only accounts have `email = NULL` and `name = NULL`; the admin list/get
  and `/api/users/me` responses carry `phone`, and admin search matches phone
  numbers. v1 keeps phone and email accounts separate (no merging or phone
  binding yet).
