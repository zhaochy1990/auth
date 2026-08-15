# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Structure

Repository for the auth backend:

- **`sources/dev/authentication-go/`** - Go backend (auth microservice). Has its own `CLAUDE.md` with detailed architecture, build commands, and conventions.
- Root `package.json` - Only holds commitlint devDependencies (not an npm workspaces setup).

## Build & Dev Commands

### Backend (run from `sources/dev/authentication-go/`)

```bash
go build ./...              # Build everything
go test ./... -count=1      # All tests; server suite needs MySQL
go test ./internal/auth/    # Pure unit tests, no MySQL
go vet ./...                # Static checks
gofmt -l .                  # Check formatting; empty output means clean
go run ./cmd/auth-service seed admin@example.com MyPassword1!  # Bootstrap admin user
```

Integration tests require MySQL running on `127.0.0.1:3306` by default. Start with `docker compose up -d mysql` from the backend directory. Override the endpoint with `TEST_MYSQL_DSN`.

The backend module uses a local `replace` for `github.com/zhaochy1990/x`. Docker builds use vendored dependencies:

```bash
go mod vendor
docker build -t auth-service-go .
```

## Commit Conventions

Uses [Conventional Commits](https://www.conventionalcommits.org/) enforced by commitlint (`@commitlint/config-conventional`). PR commits are validated in CI.

Format: `type(scope): description` - e.g., `feat(auth): add WeChat provider`, `fix(dashboard): handle token refresh`.

## Versioning & Release Pipeline

CalVer scheme: `YYYY.M.MICRO` (e.g., `2026.2.1`). The version is stored in
the root `package.json`. Backend runtime version is passed to the container as
`APP_VERSION` during deploy.

Release is automated: CI pass on `main` -> Release workflow calculates next version -> bumps version files -> creates git tag (`vYYYY.M.MICRO`) -> triggers Deploy workflow.

## CI/CD Architecture

- **CI** (`ci.yml`): Backend jobs run when
  `sources/dev/authentication-go/**` changes. CI runs `gofmt`, `go vet`,
  MySQL-backed tests, and Docker dry-run builds.
- **Release** (`release.yml`): Triggers after CI succeeds on `main`. Auto-bumps version and creates annotated tag.
- **Deploy** (`deploy.yml`): Triggers on `v*` tags. Builds the backend Docker
  image and pushes it to GHCR + Aliyun ACR (CalVer + `:latest`), then runs
  Renovate against `stride-devops` to open an `AUTH_IMAGE_TAG` bump PR.

## Deployment Topology

Single target: **Tencent Cloud** (the former Azure Container Apps + Static Web App standby stack has been retired).

- **Backend**: Docker container on a Tencent Cloud CVM, pulling the `auth-backend` image from Aliyun ACR (in-region mirror of GHCR). Uses Tencent Cloud MySQL for data persistence; the Azure Table adapter is retained only as a legacy migration/rollback source. Runs with `STORAGE_BACKEND=mysql`, the production `MYSQL_DSN`, and `MYSQL_TLS_CA_PEM` when the Tencent MySQL instance requires a custom CA. JWT keys are mounted into the container.
- **Frontend**: Owned and released from `stride-devops/admin-dashboard`.
- **Release**: GitOps via `stride-devops` (root `versions.env`); the backend
  image tag is bumped by Renovate. No cloud deploy runs from this repo.
