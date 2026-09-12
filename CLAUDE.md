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

CalVer scheme: `YYYY.M.MICRO` (e.g. `2026.9.1`), tracked per package in the
root `versions.json`. There are **no git tags** and no GitHub Releases — the
image tag is the artifact identity. The backend runtime version is passed to the
container as `APP_VERSION` at image build time.

Releases run in three phases from `.github/workflows/release.yml`, driven by the
shared `zhaochy1990/configurations/calver-release` action:

1. **bump-versions** — work out the next version of every package this push
touched. Paths come from `.github/release-packages.json`. Writes nothing and
commits nothing.
2. **build** — build and push `auth-backend` tagged with that version.
3. **commit-versions** — record the version in `versions.json`, one commit.

`commit-versions` runs last on purpose: `versions.json` must never name an image
that was never published. It is handed phase 1's result verbatim and never
recomputes, so a push landing mid-build cannot skew it.

Seed a new package with its currently deployed version, or numbering restarts
at `.1` and the deploy PR reads it as a downgrade.

## CI/CD Architecture

- **CI** (`ci.yml`): pull requests only. `commitlint`, plus `gofmt`, `go vet`,
  MySQL-backed tests and a Docker dry-run build when
  `sources/dev/authentication-go/**` changes.
- **Release** (`release.yml`): every push to `master`, with no `on.push.paths`
  filter — change detection is path-based and lives in
  `.github/release-packages.json`, so the workflow has to see the whole push. It
  runs the same lint and test as CI before building, so a failing test blocks the
  release. Then it pushes the image to Aliyun ACR, commits `versions.json`, and
  dispatches `stride-devops`'s `pin-images.yml` to open the `AUTH_IMAGE_TAG` bump
  PR. ACR only: GHCR is no longer published to, and `:latest` is no longer pushed
  — `versions.env` pins an exact tag and nothing consumes a floating one.

Note the consequence of path-based detection: a `docs`/`chore` commit inside
`sources/dev/authentication-go/` cuts a release too, unlike the old commit-type
gating.

## Deployment Topology

Primary target: **Tencent Cloud** (Azure is fully retired).

- **Backend**: Docker container on a Tencent Cloud CVM, pulling the `auth-backend` image from Aliyun ACR. Uses Tencent Cloud MySQL for data persistence. Runs with the production `MYSQL_DSN`, and `MYSQL_TLS_CA_PEM` when the Tencent MySQL instance requires a custom CA. JWT keys are mounted into the container.
- **Frontend**: Owned and released from `stride-devops/admin-dashboard`.
- **Release**: GitOps via `stride-devops` (root `versions.env`); this repo
  dispatches `stride-devops`'s `pin-images.yml`, which updates the image tag.
  No cloud deploy runs from this repo.

## Agent skills

### Issue tracker

Issues and specs for this repo live in the shared tracker repo `zhaochy1990/stride-devops` (project label `project:auth`, all gh calls with `-R`). See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-role vocabulary (`needs-triage` · `needs-info` · `ready-for-agent` · `ready-for-human` · `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `CONTEXT.md` + `docs/adr/`. See `docs/agents/domain.md`.
