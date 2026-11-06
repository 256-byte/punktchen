# LiveStop

> Working name. Repository: `punktchen`.

A map-first mobile app that shows **live departures** (bus, tram, U-Bahn, S-Bahn) directly on the stops around you in Berlin. Open the app, see every stop pole within ~500 m, each with its next departures per direction, counting down live.

**Status:** early development (Phase 0 — skeleton & tooling). Nothing runnable yet. See [`docs/PROGRESS.md`](docs/PROGRESS.md).

## Why

Departure boards are usually one stop at a time, behind a search. LiveStop puts all of them on the map at once.

It is also a portfolio project. The focus is the **backend architecture**:

- one shared poller per upstream feed, so upstream traffic does not grow with users,
- an immutable, versioned in-memory snapshot of schedule + realtime data,
- a geohash cell cache with ETags, so nearby users share one response,
- a contract-first API (OpenAPI) with generated Go and TypeScript code,
- a generic layer system, so other map data can be added later without touching the core.

## How it works

```
VBB GTFS (daily) ──► import ──► PostgreSQL + PostGIS
VBB GTFS-RT (15 s) ──► merge with schedule ──► Snapshot ──► /v1/nearby ──► mobile app
```

- **Backend:** Go, single binary (modular monolith), PostgreSQL + PostGIS, Docker Compose.
- **Mobile:** React Native + Expo, TypeScript. Countdowns are computed on the device from absolute timestamps, so the app keeps working offline.

Details: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/DATA_MODEL.md`](docs/DATA_MODEL.md), decisions in [`docs/adr/`](docs/adr/).

## Repository layout

| Path | Content |
|---|---|
| `api/` | `openapi.yaml` — the API contract |
| `backend/` | Go server (`cmd/`, `internal/`, `db/`, generated code in `gen/`) |
| `mobile/` | Expo app (`app/` screens, `src/` logic and UI) |
| `docs/` | plan, architecture, data model, progress, ADRs |

## How to run

Requirements: Docker Desktop (WSL2 on Windows), Go, Node.js, Expo Go on your phone.

**Backend + database** (PowerShell, from the repo root):

```powershell
Copy-Item .env.example .env        # optional; defaults work for local dev
docker compose up -d --build --wait
curl.exe http://localhost:8080/v1/health
```

- API: `http://localhost:8080` (reachable from the LAN for the phone)
- Metrics: `127.0.0.1:9090`, Postgres: `127.0.0.1:5432` (localhost only)
- All settings: [`.env.example`](.env.example)
- Stop: `docker compose down` (data stays in the `livestop_pgdata` volume; `down -v` deletes it)

The mobile app and the remaining run instructions follow during Phase 0 (tasks 0.7–0.8) and are kept in sync with [`CLAUDE.md` §5](CLAUDE.md#5-commands).

## Data & licence

- Transit data: [VBB](https://www.vbb.de/) GTFS and GTFS-RT, licensed **CC BY 4.0**. Data: VBB (CC BY 4.0).
- Code: [MIT](LICENSE).
