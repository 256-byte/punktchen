# 0001 — Monorepo and core stack

- **Status:** accepted
- **Date:** 2026-11-05

## Context
Solo hobby/portfolio project: Go backend, mobile app and a shared API contract. Developed on Windows; may later run on a Linux server.

## Decision
One repository with `backend/` (Go), `mobile/` (React Native + Expo), `api/openapi.yaml`, `docs/`. PostgreSQL + PostGIS for schedule data. Everything server-side runs in Docker Compose.

## Alternatives considered
- Separate repos: harder to keep contract and clients in sync for one developer.
- SQLite/SpatiaLite: simpler, but PostGIS spatial queries and `COPY` performance are a better showcase and scale to Germany-wide data.

## Consequences
Single PR can change contract + backend + app. CI must run per-folder jobs. Moving to a server = moving the compose file.
