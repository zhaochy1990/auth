# auth-service (Go + Gin)

Authentication and authorization microservice using Gin and MySQL. The service
implements OAuth2, JWT access/refresh tokens, pluggable auth providers,
membership tiers, invite codes, teams, and the admin API used by the dashboard.

The storage boundary is `internal/repository`: handlers depend only on the
repository interfaces. The runtime target is MySQL. Short-lived SMS
verification codes live in Redis (`internal/repository/redis`), which the SMS
login endpoints fail closed on (503) rather than falling back to another
store.

## Architecture

```text
cmd/auth-service/               unified cobra CLI (one binary, subcommands)
  main.go                       root command + Swagger general API info
  cmd_serve.go                  `serve`  — start the Gin HTTP server
  cmd_seed.go                   `seed`   — bootstrap the admin user + app client
  docs/                         generated OpenAPI/Swagger docs (`make swagger`)
internal/
  config/        YAML configuration (x/viper: file + env overrides)
  domain/        storage-agnostic entity models + value types
  apperror/      typed error model -> HTTP/JSON mapping
  auth/          JWT, password/client-secret hashing, PKCE, OAuth2 helpers
  repository/    storage interfaces
    mysql/       MySQL implementation and schema creation
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

Run the service locally (config comes from `config.yml`; env vars override it):

```bash
go run ./cmd/auth-service serve --config config.yml
```

Bootstrap the first admin:

```bash
go run ./cmd/auth-service seed --config config.yml admin@example.com MyPassword1!
```

## Swagger / OpenAPI

Endpoints are annotated with [swaggo/swag](https://github.com/swaggo/swag). The
generated spec lives in `cmd/auth-service/docs` (committed; regenerate with
`make swagger`). The Swagger UI is compiled in only with the `swagger` build tag
and served at `/swagger/index.html` when `swagger_enabled` is on (env override
`SWAGGER_ENABLED=true`):

```bash
make build-service-swagger      # go build -tags swagger ...
SWAGGER_ENABLED=true ./bin/auth-service serve --config config.yml
# open http://127.0.0.1:3000/swagger/index.html
```

Plain `go build` / `go test` never need the generated package (a build-tagged
no-op stub replaces the UI), so the default build stays lean.


## Tencent Cloud MySQL

Production uses a Tencent Cloud MySQL instance.
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
before the release tag is deployed — it overrides the `mysql_dsn` key in the
mounted config file. If the Tencent instance requires a custom CA, also set
`MYSQL_TLS_CA_PEM`. The Tencent Cloud container is configured with the
`MYSQL_DSN` and optional `MYSQL_TLS_CA_PEM` environment variables.

## Docker

The module uses a local `replace` for the sibling `x` library. Build images from
vendored dependencies (the image is built with `-tags swagger`, so the UI is
available at runtime when `swagger_enabled` is on):

```bash
go mod vendor
docker build -t auth-service-go .
docker compose up --build
```

The image bundles every subcommand; the default command is `serve`. Override it
to run maintenance tasks, e.g. `docker run auth-service-go seed admin@example.com`.

The repo's `config.yml` is baked into the image at `/etc/viper.yml`, so a plain
`docker run auth-service-go serve` starts with the shipped defaults. Deployments
mount their own config (over `/etc/viper.yml`, or point `CONFIG_PATH` at it) and
override secrets via environment variables.

## Configuration

Configuration is a YAML file loaded by `github.com/zhaochy1990/x/viper`
(viper-based): the file is the source of truth, and every key can be overridden
by an environment variable. The repo ships `config.yml` with all defaults; it is
baked into the Docker image at `/etc/viper.yml`. The file is resolved, in order:

1. `--config <path>` CLI flag
2. `$CONFIG_PATH` environment variable
3. `/etc/viper.yml` (the image default)

### Overriding keys with environment variables

Each YAML key maps 1:1 to the env var that overrides it — the key uppercased
(e.g. `mysql_dsn` <- `MYSQL_DSN`, `tencent_sms_secret_key` <-
`TENCENT_SMS_SECRET_KEY`). Use the env vars for secrets and per-environment
values so the same config file works everywhere.

> Env overrides apply **only to keys present in the YAML file** (viper checks
> the env per config key and does not discover env-only vars). Keep the shipped
> `config.yml` (or spell out in a deployment's custom file) every key you want
> to override — an empty value in the file is fine.
>
> The legacy `STRIDE_REQUIRE_INVITE_CODE` / `AUTH_REQUIRE_INVITE_CODE` flags are
> still honored as aliases for `require_invite_code` (new env override
> `REQUIRE_INVITE_CODE`).

### Keys

| YAML key | Env override | Default |
|----------|--------------|---------|
| `mysql_dsn` (required) | `MYSQL_DSN` | - |
| `mysql_tls_ca_pem` | `MYSQL_TLS_CA_PEM` | - |
| `mysql_tls_ca_path` | `MYSQL_TLS_CA_PATH` | - |
| `jwt_private_key_path` | `JWT_PRIVATE_KEY_PATH` | `keys/private.pem` |
| `jwt_public_key_path` | `JWT_PUBLIC_KEY_PATH` | `keys/public.pem` |
| `jwt_issuer` | `JWT_ISSUER` | `auth-service` |
| `jwt_access_token_expiry_secs` | `JWT_ACCESS_TOKEN_EXPIRY_SECS` | `3600` |
| `jwt_refresh_token_expiry_days` | `JWT_REFRESH_TOKEN_EXPIRY_DAYS` | `30` |
| `server_host` | `SERVER_HOST` | `127.0.0.1` |
| `server_port` | `SERVER_PORT` | `3000` |
| `cors_allowed_origins` | `CORS_ALLOWED_ORIGINS` | `http://localhost:5173,http://localhost:3000` |
| `auth_enable_test_providers` | `AUTH_ENABLE_TEST_PROVIDERS` | `false` |
| `swagger_enabled` | `SWAGGER_ENABLED` | `false` (UI also requires the `swagger` build tag) |
| `require_invite_code` | `REQUIRE_INVITE_CODE` | `false` (legacy aliases `STRIDE_REQUIRE_INVITE_CODE` / `AUTH_REQUIRE_INVITE_CODE`) |
| `redis_addr` | `REDIS_ADDR` | `127.0.0.1:6379` |
| `redis_password` | `REDIS_PASSWORD` | - |
| `redis_db` | `REDIS_DB` | `0` |
| `auth_sms_test_mode` | `AUTH_SMS_TEST_MODE` | `false` (fixes the code at `123456`, skips Tencent) |
| `tencent_sms_secret_id` | `TENCENT_SMS_SECRET_ID` | - |
| `tencent_sms_secret_key` | `TENCENT_SMS_SECRET_KEY` | - |
| `tencent_sms_sdk_app_id` | `TENCENT_SMS_SDK_APP_ID` | - |
| `tencent_sms_sign_name` | `TENCENT_SMS_SIGN_NAME` | - |
| `tencent_sms_template_id` | `TENCENT_SMS_TEMPLATE_ID` | - |
| `tencent_sms_region` | `TENCENT_SMS_REGION` | `ap-guangzhou` |
| `sms_send_rate_limit` | `SMS_SEND_RATE_LIMIT` | `10` (per-IP sends per hour) |
| `sms_verify_rate_limit` | `SMS_VERIFY_RATE_LIMIT` | `60` (per-IP verifies per hour) |
| `wechat_code2session_url` | `WECHAT_CODE2SESSION_URL` | WeChat's public `jscode2session` endpoint (tests) |
| `app_version` | `APP_VERSION` | `dev` |
| `log_level` / `log_format` | `LOG_LEVEL` / `LOG_FORMAT` | `debug` / `json` (`log_format` is `json` for structured output or `console` for human-readable; absent/unset falls back to `console`) |

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

WeChat credentials are configured per application as a provider config via the
admin API — `POST /admin/applications/{id}/providers` with
`{"provider_id":"wechat","config":{"appid":"...","secret":"..."}}` —
stored on the application's `auth_app_providers` row. The `token_exchange`
flow reads `appid`/`secret` from that provider config; the legacy
application-level `wechat_app_id` / `wechat_app_secret` columns were removed
from the auth backend (schema, domain, admin API, and repository) and any
pre-existing values were migrated into the provider config. Credentials are
**not** read from environment variables.

`token_exchange` is the **only** WeChat login path. The generic provider login
(`/api/auth/provider/{provider_id}/login`) and WeChat account-linking no longer
serve WeChat: a `provider_id=wechat` request is rejected with
`provider_not_supported`.

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
- Missing `TENCENT_SMS_*` config keys do not prevent startup; `send` returns
  `400 sms_not_configured`. Set `auth_sms_test_mode: true` (env override
  `AUTH_SMS_TEST_MODE=true`) to run the whole flow with the fixed code `123456`
  and no Tencent call (demos / CI).
- When `require_invite_code` is on (env override `REQUIRE_INVITE_CODE`; legacy
  `STRIDE_REQUIRE_INVITE_CODE` / `AUTH_REQUIRE_INVITE_CODE` aliases still work),
  a **new** phone must present a valid `invite_code` in the verify request
  (reusing the existing single-use consumption and membership/user-type
  grants); the invite is never required for existing accounts.
- Error codes: `sms_code_invalid`, `sms_code_expired`, `sms_attempts_exceeded`,
  `sms_send_cooldown`, `sms_daily_limit`, `sms_not_configured`.
- Phone-only accounts have `email = NULL` and `name = NULL`; the admin list/get
  and `/api/users/me` responses carry `phone`, and admin search matches phone
  numbers. v1 keeps phone and email accounts separate (no merging or phone
  binding yet).
