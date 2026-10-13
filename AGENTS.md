# AGENTS.md

Guidance for coding agents working in this repository.

## Backend Source Of Truth

- `sources/dev/authentication-go/` is the maintained auth backend. Make backend
  implementation, test, routing, and documentation changes there by default.

## Frontend

- The React/TypeScript admin UI and its CI/CD live in the `stride-devops`
  repository under `admin-dashboard/`. This repository owns the auth backend
  only.

## 分支纪律

默认直接在当前分支上修改，不要新建分支或 git worktree；仅当用户明确要求时才创建。
