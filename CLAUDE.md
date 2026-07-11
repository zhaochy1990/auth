# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Structure

Monorepo with two applications:

- **`sources/dev/authentication-go/`** - Go backend (auth microservice). Has its own `CLAUDE.md` with detailed architecture, build commands, and conventions.
- **`sources/dev/admin-dashboard/`** - React/TypeScript frontend (admin UI).
- Root `package.json` - Only holds commitlint devDependencies (not an npm workspaces setup).

## Build & Dev Commands

### Backend (run from `sources/dev/authentication-go/`)

```bash
go build ./...              # Build everything
go test ./... -count=1      # All tests; server suite needs Azurite
go test ./internal/auth/    # Pure unit tests, no Azurite
go vet ./...                # Static checks
gofmt -l .                  # Check formatting; empty output means clean
go run ./cmd/auth-service seed admin@example.com MyPassword1!  # Bootstrap admin user
```

Integration tests require Azurite (Azure Storage emulator) running on port 10002. Start with `docker compose up azurite` from the backend directory. The tests generate and read JWT keys from `keys/private.pem` and `keys/public.pem`; CI creates ephemeral keys before running tests.

The backend module uses a local `replace` for `github.com/zhaochy1990/x`. Docker builds use vendored dependencies:

```bash
go mod vendor
docker build -t auth-service-go .
```

### Frontend (run from `sources/dev/admin-dashboard/`)

```bash
npm ci           # Install dependencies
npm run dev      # Vite dev server
npm run build    # tsc + vite build
npm run lint     # ESLint
```

Build-time env vars: `VITE_API_CLIENT_ID`, `VITE_API_BASE_URL`, `VITE_APP_VERSION`.

## Commit Conventions

Uses [Conventional Commits](https://www.conventionalcommits.org/) enforced by commitlint (`@commitlint/config-conventional`). PR commits are validated in CI.

Format: `type(scope): description` - e.g., `feat(auth): add WeChat provider`, `fix(dashboard): handle token refresh`.

## Versioning & Release Pipeline

CalVer scheme: `YYYY.M.MICRO` (e.g., `2026.2.1`). Version is synchronized across `package.json` (root) and `sources/dev/admin-dashboard/package.json`. Backend runtime version is passed to the container as `APP_VERSION` during deploy.

Release is automated: CI pass on `main` -> Release workflow calculates next version -> bumps version files -> creates git tag (`vYYYY.M.MICRO`) -> triggers Deploy workflow.

## CI/CD Architecture

- **CI** (`ci.yml`): Uses path filtering - backend jobs run when `sources/dev/authentication-go/**` changes, frontend jobs run when `sources/dev/admin-dashboard/**` changes. Backend CI runs `gofmt`, `go vet`, Azurite-backed tests, and Docker dry-run builds.
- **Release** (`release.yml`): Triggers after CI succeeds on `main`. Auto-bumps version and creates annotated tag.
- **Deploy** (`deploy.yml`): Triggers on `v*` tags. Backend -> Go Docker build -> GHCR -> Azure Container Apps. Frontend -> Vite build -> Azure Static Web Apps.

## Deployment Topology

- **Backend**: Docker container on Azure Container Apps, pulling from GHCR (`ghcr.io/<owner>/auth-backend`). Uses Azure Table Storage for data persistence. JWT keys stored in Azure File Share mounted into the container.
- **Frontend**: Azure Static Web Apps (SPA with `navigationFallback` rewrite to `index.html`).
- **Auth**: GitHub OIDC federated credentials for Azure (no stored Azure secrets in GitHub).

## Frontend Architecture

React 19 + TypeScript + Vite + Tailwind CSS 4. Key libraries:
- **State**: Zustand (`store/authStore.ts`)
- **Data fetching**: TanStack React Query + Axios (`api/client.ts`, `api/admin.ts`)
- **Routing**: React Router v7 (`router/`)
- **i18n**: i18next + react-i18next (`i18n/`)
- **UI**: Lucide icons, react-hot-toast

Pages: `LoginPage`, `DashboardPage`, `NotFoundPage`, plus feature pages under `pages/applications/` and `pages/users/`.
