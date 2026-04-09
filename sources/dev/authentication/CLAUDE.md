# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Rust authentication/authorization microservice using Axum 0.7 and Azure Table Storage. Implements OAuth2 with JWT (RS256), pluggable auth providers, and PKCE support.

## Build, Test, Lint

```bash
cargo build                                            # Debug build
cargo test --features test-providers -- --test-threads=1  # Run all tests (serial)
cargo test --features test-providers --test admin_test -- --test-threads=1  # Run one test file
cargo test --features test-providers -- test_name --test-threads=1         # Run a single test
cargo clippy                                           # Lint
cargo fmt --check                                      # Check formatting
cargo fmt                                              # Auto-format
```

The `test-providers` feature flag enables the `"test"` auth provider used by integration tests. Always include it when running tests.

Tests require Azurite (Azure Storage emulator) running locally on port 10002. Start with Docker:
```bash
docker run -p 10002:10002 mcr.microsoft.com/azure-storage/azurite azurite-table --tableHost 0.0.0.0
```

Or use docker-compose:
```bash
docker compose up azurite
```

Optionally set `TEST_STORAGE_CONNECTION_STRING` if not using default Azurite endpoint.

## Architecture

**Single crate** (`src/`): Handlers, auth logic, routing, config, storage layer.

**Request flow:**
```
HTTP Request → Routes (routes.rs) → Axum Extractors (auth/middleware.rs) → Handlers (handlers/) → Repository trait → Azure Table Storage
```

**Storage layer** (`src/db/`):
- `repository.rs` — Trait-based storage abstraction (6 sub-traits + composite `Repository` trait)
- `azure_tables.rs` — Azure Table Storage implementation with index rows for secondary lookups
- `models.rs` — Plain Rust structs for all 6 entity types

**Azure Table Storage tables** (prefixed `auth` for shared storage account):
- `authapplications` — OAuth2 applications with client_id and name indexes
- `authusers` — Users with email index
- `authaccounts` — User-provider account links with provider-account and ID indexes
- `authappproviders` — Provider configurations per app with ID index
- `authauthcodes` — OAuth2 authorization codes
- `authrefreshtokens` — Refresh tokens with token hash index

**Pluggable Auth Providers:** The `AuthProvider` trait (`auth/providers/mod.rs`) defines the interface for authentication methods. `create_provider()` is the factory function. Current providers: password, wechat, test (feature-gated). Add new providers by implementing the trait and adding a match arm in the factory.

**Axum Extractors for Auth** (`auth/middleware.rs`): Custom `FromRequestParts` implementations handle authentication:
- `AuthenticatedUser` — Bearer token validation
- `ClientApp` — X-Client-Id header with app lookup
- `AuthenticatedApp` — Basic auth for OAuth2 clients (client_id:secret)
- `AdminAuth` — Bearer token with `role: "admin"` in JWT claims

**Error Handling:** `AppError` enum (`error.rs`) implements `IntoResponse` to map errors to HTTP status codes and consistent JSON error bodies. Database errors use `AppError::Database(String)`.

## API Endpoints

| Prefix | Auth Method | Endpoints |
|--------|-------------|-----------|
| `/admin/*` | `Authorization: Bearer` (admin role) | Application CRUD, provider management, user management |
| `/api/auth/*` | `X-Client-Id` header | Register, login, refresh, logout |
| `/api/users/*` | `Authorization: Bearer` | Profile, accounts |
| `/oauth/*` | `Authorization: Basic` (client_id:secret) | Token, revoke, introspect |
| `/health` | None | Health check |

## Testing Architecture

- Integration tests use `tower::ServiceExt::oneshot` (in-process, no HTTP server)
- Tests connect to Azurite (Azure Storage emulator) for Azure Table Storage
- Each `TestApp::new()` clears and recreates all tables for isolation
- All tests use `#[serial]` from `serial_test` crate and run with `--test-threads=1`
- `TestApp::new()` bootstraps an admin user + app via `seed::bootstrap()`
- `TestApp.admin_token` provides a pre-issued Bearer token with admin role for admin API calls in tests
- JWT keys are read from `keys/` relative to project root
- Test helpers live in `tests/common/mod.rs` (TestApp, TestResponse)

## Key Conventions

- Route path parameters use `:param` syntax (axum 0.7.x canonical form)
- `AppState` lives in `lib.rs` and implements `AsRef<AppState>` for extractor compatibility
- `AppState.repo` is `Arc<dyn Repository>` — all storage access goes through the trait abstraction
- JWT `verify_access_token` sets `validate_aud = false` (jsonwebtoken 9.3 requires this when no expected audience is configured)
- The `test-providers` Cargo feature gates `src/auth/providers/test_provider.rs` and the `"test"` arm in `create_provider()`
- Azure Storage connection strings use standard format: `DefaultEndpointsProtocol=https;AccountName=...;AccountKey=...;EndpointSuffix=core.windows.net`

## Admin Bootstrap

Admin access uses the standard JWT authentication flow (Bearer token with `role: "admin"` in claims). There is no static API key.

**Bootstrapping the first admin** uses the `seed` CLI command:

```bash
AZURE_STORAGE_CONNECTION_STRING="..." cargo run -- seed admin@example.com MyPassword1!
```

This creates:
1. An "Admin Dashboard" application (with client_id + client_secret)
2. An admin user with the given email/password
3. A "password" auth provider on the app

The command is idempotent — re-running with the same email promotes an existing user or reports "already_admin". The client_secret is only shown on first creation.

**In tests**, `TestApp::new()` automatically calls `seed::bootstrap()` to set up an admin user (`test-admin@internal`) and stores a pre-issued admin JWT in `TestApp.admin_token`.

## Environment Variables

| Variable | Required | Default |
|----------|----------|---------|
| `AZURE_STORAGE_CONNECTION_STRING` | Yes | - |
| `JWT_PRIVATE_KEY_PATH` | No | `keys/private.pem` |
| `JWT_PUBLIC_KEY_PATH` | No | `keys/public.pem` |
| `JWT_ISSUER` | No | `auth-service` |
| `JWT_ACCESS_TOKEN_EXPIRY_SECS` | No | `3600` |
| `JWT_REFRESH_TOKEN_EXPIRY_DAYS` | No | `30` |
| `SERVER_HOST` | No | `127.0.0.1` |
| `SERVER_PORT` | No | `3000` |
| `CORS_ALLOWED_ORIGINS` | No | `http://localhost:5173,http://localhost:3000` |
