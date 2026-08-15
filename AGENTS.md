# AGENTS.md

Guidance for coding agents working in this repository.

## Backend Source Of Truth

- `sources/dev/authentication-go/` is the maintained auth backend. Make backend
  implementation, test, routing, and documentation changes there by default.

## Frontend

- The React/TypeScript admin UI and its CI/CD live in the `stride-devops`
  repository under `admin-dashboard/`. This repository owns the auth backend
  only.
