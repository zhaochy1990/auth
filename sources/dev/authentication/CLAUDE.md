# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Rust authentication/authorization microservice using Axum 0.7, SeaORM 1, and SQLite. Implements OAuth2 with JWT (RS256), pluggable auth providers, and PKCE support.

## Build, Test, Lint

```bash
cargo build                                            # Debug build
cargo test --features test-providers                   # Run all tests (56 tests)
cargo test --features test-providers --test admin_test # Run one test file
cargo test --features test-providers -- test_name      # Run a single test
cargo clippy                                           # Lint
cargo fmt --check                                      # Check formatting
cargo fmt                                              # Auto-format
```

The `test-providers` feature flag enables the `"test"` auth provider used by integration tests. Always include it when running tests.

## Architecture

**Cargo Workspace** with 3 crates:
- Root crate (`src/`): Handlers, auth logic, routing, config
- `entity/`: SeaORM entity definitions (user, account, application, app_provider, authorization_code, refresh_token)
- `migration/`: SeaORM database migrations

**Request flow:**
```
HTTP Request → Routes (routes.rs) → Axum Extractors (auth/middleware.rs) → Handlers (handlers/) → SeaORM → SQLite
```

**Pluggable Auth Providers:** The `AuthProvider` trait (`auth/providers/mod.rs`) defines the interface for authentication methods. `create_provider()` is the factory function. Current providers: password, wechat, test (feature-gated). Add new providers by implementing the trait and adding a match arm in the factory.

**Axum Extractors for Auth** (`auth/middleware.rs`): Custom `FromRequestParts` implementations handle authentication:
- `AuthenticatedUser` — Bearer token validation
- `ClientApp` — X-Client-Id header with app lookup
- `AuthenticatedApp` — Basic auth for OAuth2 clients (client_id:secret)
- `AdminAuth` — X-Admin-Key header validation

**Error Handling:** `AppError` enum (`error.rs`) implements `IntoResponse` to map errors to HTTP status codes and consistent JSON error bodies.

## API Endpoints

| Prefix | Auth Method | Endpoints |
|--------|-------------|-----------|
| `/admin/*` | `X-Admin-Key` header | Application CRUD, provider management |
| `/api/auth/*` | `X-Client-Id` header | Register, login, refresh, logout |
| `/api/users/*` | `Authorization: Bearer` | Profile, accounts |
| `/oauth/*` | `Authorization: Basic` (client_id:secret) | Token, revoke, introspect |
| `/health` | None | Health check |

## Testing Architecture

- Integration tests use `tower::ServiceExt::oneshot` (in-process, no HTTP server)
- Each test gets a fresh SQLite `::memory:` database with migrations applied
- Tests run in parallel and are fully isolated
- `TestApp::new()` constructs Config directly (no env vars needed)
- JWT keys are read from `keys/` relative to project root
- Test helpers live in `tests/common/mod.rs` (TestApp, TestResponse)

## Key Conventions

- Route path parameters use `:param` syntax (axum 0.7.x canonical form)
- `AppState` lives in `lib.rs` and implements `AsRef<AppState>` for extractor compatibility
- JWT `verify_access_token` sets `validate_aud = false` (jsonwebtoken 9.3 requires this when no expected audience is configured)
- The `test-providers` Cargo feature gates `src/auth/providers/test_provider.rs` and the `"test"` arm in `create_provider()`

## Environment Variables

| Variable | Required | Default |
|----------|----------|---------|
| `DATABASE_URL` | Yes | - |
| `ADMIN_API_KEY` | Yes | - |
| `JWT_PRIVATE_KEY_PATH` | No | `keys/private.pem` |
| `JWT_PUBLIC_KEY_PATH` | No | `keys/public.pem` |
| `JWT_ISSUER` | No | `auth-service` |
| `JWT_ACCESS_TOKEN_EXPIRY_SECS` | No | `3600` |
| `JWT_REFRESH_TOKEN_EXPIRY_DAYS` | No | `30` |
| `SERVER_HOST` | No | `127.0.0.1` |
| `SERVER_PORT` | No | `3000` |
