# Copilot instructions

## Repository shape

This is a two-app monorepo, not an npm workspace. The root `package.json` only contains commitlint dependencies.

- `sources\dev\authentication` is the Rust auth service.
- `sources\dev\admin-dashboard` is the React/TypeScript admin UI.

## Build, test, and lint commands

Run backend commands from `sources\dev\authentication`:

```powershell
cargo build
cargo fmt --check
cargo clippy -- -D warnings
cargo run -- seed admin@example.com MyPassword1!
```

Backend tests require Azurite Table Storage on port 10002, RSA keys in `keys\private.pem` and `keys\public.pem`, the `test-providers` feature, and serial execution:

```powershell
docker compose up azurite
cargo test --features test-providers -- --test-threads=1
cargo test --features test-providers --test admin_test -- --test-threads=1
cargo test --features test-providers -- test_name --test-threads=1
```

If local test keys are missing, mirror CI's key generation before running tests:

```powershell
New-Item -ItemType Directory -Force keys
openssl genpkey -algorithm RSA -out keys\private.pem -pkeyopt rsa_keygen_bits:2048
openssl rsa -pubout -in keys\private.pem -out keys\public.pem
```

Run frontend commands from `sources\dev\admin-dashboard`:

```powershell
npm ci
npm run dev
npm run lint
npm run build
npm run preview
```

Playwright MCP is configured in `.vscode\mcp.json` for browser-driven frontend checks. Start the Vite dev server with `npm run dev` from `sources\dev\admin-dashboard` before using it against the admin UI.

Run commit message validation from the repository root:

```powershell
npm ci
npx commitlint --from <base-sha> --to <head-sha> --verbose
```

## High-level architecture

### Backend

The backend is a single Axum crate. `src\main.rs` loads `.env`, builds `Config`, creates an `AzureTableRepository`, initializes JWT support, and passes `AppState` into `routes::create_router`. `AppState` contains `Arc<dyn Repository>`, `JwtManager`, and `Config`; it implements `AsRef<AppState>` so custom extractors can access shared state.

The request flow is:

```text
HTTP request -> routes.rs -> auth extractors -> handlers -> Repository trait -> Azure Table Storage
```

Route groups have distinct authentication:

| Route prefix | Auth mechanism | Purpose |
| --- | --- | --- |
| `/api/auth` | `X-Client-Id` via `ClientApp` | Register, login, provider login, refresh, logout |
| `/api/users` | Bearer access token via `AuthenticatedUser` | User profile and account linking |
| `/api/teams` | Bearer access token via `AuthenticatedUser` | User-facing team operations |
| `/oauth` | HTTP Basic client credentials via `AuthenticatedApp` | Token, revoke, introspect |
| `/admin` | Bearer access token with `role == "admin"` via `AdminAuth` | Admin dashboard APIs |
| `/health` | None | Health check with app version |

Storage is abstracted behind sub-traits in `src\db\repository.rs` and implemented by `src\db\azure_tables.rs`. Domain entities currently cover applications, users, linked accounts, per-application providers, authorization codes, refresh tokens, invite codes, teams, and team memberships. Azure table names are prefixed with `auth` for shared storage accounts; secondary lookups are implemented with explicit index rows in the table implementation.

Auth provider login is pluggable through the `AuthProvider` trait and `create_provider()` factory in `src\auth\providers\mod.rs`. The `"test"` provider is feature-gated behind `test-providers`; normal password registration/login uses the password helpers rather than the provider factory.

### Frontend

The admin dashboard uses React 19, Vite, TypeScript, Tailwind CSS 4, React Router v7, TanStack Query, Axios, Zustand, and i18next.

`src\main.tsx` creates the React Query client and global toast behavior. `src\App.tsx` hydrates auth state from `sessionStorage`. `src\router\index.tsx` protects the dashboard layout and wires pages for dashboard, applications, users, invite codes, and teams.

`src\api\client.ts` owns Axios configuration. It attaches Bearer tokens for `/admin/*` and `/api/teams/*`, refreshes access tokens on protected 401 responses, and redirects to `/login` after refresh failure. `src\store\authStore.ts` handles login/logout/hydration and requires decoded JWTs to have `role === "admin"` for dashboard access.

Feature pages keep API calls in `src\api\admin.ts`, shared TypeScript contracts in `src\api\types.ts`, and translations under both `src\i18n\locales\zh-CN` and `src\i18n\locales\en-US`. The saved language defaults to `zh-CN`; fallback language is `en-US`.

## Key conventions

- Use Axum 0.7 `:param` route parameters in backend routes.
- Keep all storage access behind `Repository` and its sub-traits; handlers should not depend directly on Azure Table clients.
- Return API errors through `AppError`, which maps errors to status codes and consistent JSON bodies.
- Backend integration tests use `tower::ServiceExt::oneshot` in process, `TestApp::new()` from `tests\common\mod.rs`, `#[serial]`, Azurite, and `--test-threads=1`. `TestApp::new()` clears and recreates all tables and bootstraps an admin app/user through `seed::bootstrap()`.
- Admin access is normal JWT auth with an admin role claim; there is no static admin API key.
- Client secrets are only exposed at creation, rotation, or bootstrap time; store hashes in persisted application records.
- Fields such as redirect URIs, scopes, provider config, and metadata are often stored as JSON strings in backend models/Azure rows but exposed as arrays or objects in HTTP/frontend types.
- Frontend mutations use TanStack Query invalidation for the affected query keys and rely on the global mutation error toast configured in `main.tsx`.
- UI text should use i18next namespaces; add keys for both `zh-CN` and `en-US` when adding or changing visible strings.
- When adding a cross-cutting feature, update all relevant surfaces together: backend model/repository/table implementation/handler/routes/tests, then frontend API types/client functions/routes/sidebar/pages/i18n as applicable.
- CI path filters only run backend jobs for `sources\dev\authentication\**` changes and frontend jobs for `sources\dev\admin-dashboard\**` changes. Backend CI uses `RUSTFLAGS=-Dwarnings`, `cargo fmt --check`, `cargo clippy -- -D warnings`, and Azurite-backed tests.
- Versions use CalVer `YYYY.M.MICRO` and are synchronized across root `package.json`, `sources\dev\admin-dashboard\package.json`, and `sources\dev\authentication\Cargo.toml` by the release workflow.
- Commit messages follow Conventional Commits, enforced by commitlint; use `type(scope): description` such as `feat(auth): add provider`.

## Deployment context

Release runs after CI succeeds on `main`, bumps versions, creates a `vYYYY.M.MICRO` tag, and triggers deployment. Deployment builds the backend Docker image from `sources\dev\authentication`, pushes it to GHCR, updates Azure Container Apps, then builds the frontend with `VITE_API_CLIENT_ID`, `VITE_API_BASE_URL`, and `VITE_APP_VERSION` for Azure Static Web Apps.
