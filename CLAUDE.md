# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Structure

Monorepo with two applications:

- **`sources/dev/authentication/`** — Rust backend (auth microservice). Has its own `CLAUDE.md` with detailed architecture, build commands, and conventions — read it when working on backend code.
- **`sources/dev/admin-dashboard/`** — React/TypeScript frontend (admin UI).
- Root `package.json` — Only holds commitlint devDependencies (not an npm workspaces setup).

## Build & Dev Commands

### Backend (run from `sources/dev/authentication/`)

```bash
cargo build                                                          # Debug build
cargo test --features test-providers -- --test-threads=1             # All tests (serial, needs Azurite)
cargo test --features test-providers --test admin_test -- --test-threads=1  # Single test file
cargo test --features test-providers -- test_name --test-threads=1   # Single test by name
cargo clippy -- -D warnings                                          # Lint (CI treats warnings as errors)
cargo fmt --check                                                    # Check formatting
cargo run -- seed admin@example.com MyPassword1!                     # Bootstrap admin user
```

Tests require Azurite (Azure Storage emulator) running on port 10002. Start with `docker compose up azurite` from the backend directory. The `test-providers` feature flag is always required when running tests. All tests run serially (`--test-threads=1`).

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

Format: `type(scope): description` — e.g., `feat(auth): add WeChat provider`, `fix(dashboard): handle token refresh`.

## Versioning & Release Pipeline

CalVer scheme: `YYYY.M.MICRO` (e.g., `2026.2.1`). Version is synchronized across `package.json` (root), `sources/dev/admin-dashboard/package.json`, and `sources/dev/authentication/Cargo.toml`.

Release is automated: CI pass on `main` → Release workflow calculates next version → bumps version files → creates git tag (`vYYYY.M.MICRO`) → triggers Deploy workflow.

## CI/CD Architecture

- **CI** (`ci.yml`): Uses path filtering — backend jobs only run when `sources/dev/authentication/**` changes, frontend jobs only when `sources/dev/admin-dashboard/**` changes. `RUSTFLAGS=-Dwarnings` makes clippy warnings fail the build. Backend tests run against Azurite.
- **Release** (`release.yml`): Triggers after CI succeeds on `main`. Auto-bumps version and creates annotated tag.
- **Deploy** (`deploy.yml`): Triggers on `v*` tags. Backend → Docker build → GHCR → Azure Container Apps. Frontend → Vite build → Azure Static Web Apps.

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
