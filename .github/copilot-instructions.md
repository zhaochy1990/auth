# Copilot instructions

## Repository shape

This is a two-app monorepo, not an npm workspace. The root `package.json` only contains commitlint dependencies.

- `sources\dev\authentication-go` is the Go auth service.
- `sources\dev\admin-dashboard` is the React/TypeScript admin UI.

## Build, test, and lint commands

Run backend commands from `sources\dev\authentication-go`:

```powershell
go build ./...
gofmt -l .
go vet ./...
go run ./cmd/auth-service seed admin@example.com MyPassword1!
```

Backend integration tests require MySQL on `127.0.0.1:3306` by default:

```powershell
docker compose up -d mysql
$env:TEST_MYSQL_DSN = "mysql://auth:auth_password@127.0.0.1:3306/auth_test"
go test ./... -count=1
go test ./internal/auth/ -count=1
go test ./internal/server/ -run TestHealth -v -count=1
Remove-Item Env:TEST_MYSQL_DSN
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

The backend is a Go + Gin service. `cmd\auth-service\main.go` loads config, opens the configured repository backend, initializes JWT support, and wires routes through `internal\server.NewRouter`.

The request flow is:

```text
HTTP request -> Gin route group -> auth middleware -> handlers -> Repository interface -> MySQL
```

Route groups have distinct authentication:

| Route prefix | Auth mechanism | Purpose |
| --- | --- | --- |
| `/api/auth` | `X-Client-Id` via client middleware | Register, login, provider login, refresh, logout |
| `/api/users` | Bearer access token | User profile and account linking |
| `/api/teams` | Bearer access token | User-facing team operations |
| `/oauth` | HTTP Basic client credentials | Token, revoke, introspect |
| `/admin` | Bearer access token with `role == "admin"` | Admin dashboard APIs |
| `/health` | None | Health check with app version |

Storage is abstracted behind interfaces in `internal\repository\repository.go`. The runtime target is `internal\repository\mysql`; `internal\repository\aztables` is retained as a legacy migration/rollback source. Domain entities cover applications, users, linked accounts, per-application providers, authorization codes, refresh tokens, invite codes, teams, and team memberships.

Auth provider login is pluggable through `internal\auth\providers`. The `test` provider is gated by `AUTH_ENABLE_TEST_PROVIDERS`; normal password registration/login uses password helpers rather than the provider factory.

### Frontend

The admin dashboard uses React 19, Vite, TypeScript, Tailwind CSS 4, React Router v7, TanStack Query, Axios, Zustand, and i18next.

`src\main.tsx` creates the React Query client and global toast behavior. `src\App.tsx` hydrates auth state from `sessionStorage`. `src\router\index.tsx` protects the dashboard layout and wires pages for dashboard, applications, users, invite codes, and teams.

`src\api\client.ts` owns Axios configuration. It attaches Bearer tokens for `/admin/*` and `/api/teams/*`, refreshes access tokens on protected 401 responses, and redirects to `/login` after refresh failure. `src\store\authStore.ts` handles login/logout/hydration and requires decoded JWTs to have `role === "admin"` for dashboard access.

Feature pages keep API calls in `src\api\admin.ts`, shared TypeScript contracts in `src\api\types.ts`, and translations under both `src\i18n\locales\zh-CN` and `src\i18n\locales\en-US`. The saved language defaults to `zh-CN`; fallback language is `en-US`.

## Key conventions

- Keep all storage access behind `repository.Repository` and its sub-interfaces; handlers should not depend directly on MySQL or Azure clients.
- Return API errors through `apperror.Error`, which maps errors to status codes and consistent JSON bodies.
- Backend integration tests use Gin's in-process HTTP engine, `newTestApp` from `internal\server\integration_test.go`, MySQL, and `go test ./... -count=1`. `newTestApp` clears all MySQL tables and bootstraps an admin app/user through `seed.Bootstrap`.
- Admin access is normal JWT auth with an admin role claim; there is no static admin API key.
- Client secrets are only exposed at creation, rotation, or bootstrap time; store hashes in persisted application records.
- Fields such as redirect URIs, scopes, provider config, and metadata are often stored as JSON strings in backend models/MySQL rows but exposed as arrays or objects in HTTP/frontend types.
- Frontend mutations use TanStack Query invalidation for the affected query keys and rely on the global mutation error toast configured in `main.tsx`.
- UI text should use i18next namespaces; add keys for both `zh-CN` and `en-US` when adding or changing visible strings.
- When adding a cross-cutting feature, update all relevant surfaces together: backend model/repository/table implementation/handler/routes/tests, then frontend API types/client functions/routes/sidebar/pages/i18n as applicable.
- CI path filters run backend jobs for `sources\dev\authentication-go\**` changes and frontend jobs for `sources\dev\admin-dashboard\**` changes. Backend CI uses `gofmt`, `go vet`, MySQL-backed `go test`, and Docker dry-run builds.
- Versions use CalVer `YYYY.M.MICRO` and are synchronized across root `package.json` and `sources\dev\admin-dashboard\package.json` by the release workflow. Backend runtime version is injected with `APP_VERSION` during deployment.
- Commit messages follow Conventional Commits, enforced by commitlint; use `type(scope): description` such as `feat(auth): add provider`.

## Deployment context

Release runs after CI succeeds on `main`, bumps versions, creates a `vYYYY.M.MICRO` tag, and triggers deployment. Deployment vendors the Go backend dependencies, builds the Docker image from `sources\dev\authentication-go`, pushes it to GHCR, updates Azure Container Apps, then builds the frontend with `VITE_API_CLIENT_ID`, `VITE_API_BASE_URL`, and `VITE_APP_VERSION` for Azure Static Web Apps.
