# CLAUDE.md — LiveStop

> Working name: **LiveStop**. A map-first mobile app that shows live departures (bus, tram, U-Bahn, S-Bahn) directly on the stops around the user in Berlin.
> Hobby / portfolio project. The **backend architecture is the showcase**: clean, efficient, well-tested, well-documented.

## 0. Read this first, every session

Before doing anything, read in this order:

1. `docs/PROGRESS.md` — where we are, what is next, open questions.
2. `docs/PLAN.md` — the phase you are in and its tasks + acceptance criteria.
3. `docs/ARCHITECTURE.md` — how the system fits together.
4. `docs/DATA_MODEL.md` — data structures, IDs, API shapes.
5. `docs/adr/` — why things are the way they are.

If something you are asked to do contradicts these documents, **stop and ask the user** instead of improvising.

## 1. Golden rules (non-negotiable)

1. **Stay in the current phase.** Only work on tasks of the phase marked active in `docs/PROGRESS.md`. Do not start later phases "because it is easy now".
2. **One task at a time.** Finish it (code + tests + docs), update `docs/PROGRESS.md`, then move on.
3. **No architecture change without an ADR.** New component, new storage, new protocol, swapping a library listed in the stack → write `docs/adr/NNNN-title.md` first and ask the user to approve it.
4. **No new dependency without a reason.** Prefer the standard library. If you add a dependency, note it and the reason in the PROGRESS session log.
5. **API is contract-first.** Change `api/openapi.yaml` first, then regenerate Go + TypeScript code. Never hand-edit generated files.
6. **The mobile app never talks to VBB or any third-party data source.** It only talks to our backend.
7. **Respect upstream limits.** The VBB GTFS-RT feed allows max 7 requests/minute (since 2026-11-02; check https://production.gtfsrt.vbb.de/ for changes). We poll it from exactly one place (the ingestor), never per user request, with compressed responses and a User-Agent that names the project.
8. **Times are absolute.** The API sends ISO-8601 timestamps with offset (`2026-11-05T14:32:00+01:00`). Countdowns are computed on the device. Never send "3 min".
9. **Generic layers, single implementation.** The Provider/Layer system is generic (see ARCHITECTURE §3.5 backend, §4.4 mobile), but only the `transit` layer is implemented. Do not build `toilets`, `alerts` etc. before their phase.
10. **`mobile/src/core` is pure TypeScript.** It must not import `react`, `react-native` or `expo-*`. Enforced by ESLint.
11. **Never commit downloaded data.** GTFS zips, extracted CSVs, DB dumps stay out of git (see `.gitignore`). Small recorded fixtures for tests go in `backend/testdata/` and must stay under 5 MB each.
12. **Tests are part of done.** Every task in PLAN.md has acceptance criteria; they must be verifiable by a test or a documented manual check.
13. **Do not guess about the data.** VBB data has quirks (ID formats, extended route types, ring lines, times after 24:00). When unsure, inspect the real data and document the finding in `docs/DATA_MODEL.md` §7 "Verified facts".

## 2. Communication

- Talk to the user in **Turkish**.
- Write code, comments, commit messages and all docs in **English** (public portfolio repo).
- When a task is done, report: what changed, how it was tested, what is next. Short.
- Ask before: deleting files, rewriting history, changing an ADR, adding a phase.

## 3. Stack (summary — details in ARCHITECTURE.md)

| Area | Choice |
|---|---|
| Backend | Go (latest stable), `net/http`, `pgx/v5`, `sqlc`, `golang-migrate`, `oapi-codegen`, MobilityData `gtfs-realtime-bindings`, `mmcloughlin/geohash`, `log/slog`, `prometheus/client_golang`, `testcontainers-go` |
| Database | PostgreSQL + PostGIS (Docker) |
| Mobile | React Native + Expo (latest SDK), Expo Router, TypeScript strict, `react-native-maps`, `expo-location`, TanStack Query (+ AsyncStorage persister), Zustand, `openapi-typescript` + `openapi-fetch`, Reanimated, Gesture Handler, `@gorhom/bottom-sheet` |
| Contract | `api/openapi.yaml` (OpenAPI 3.1) |
| Dev env | Windows + Docker Desktop (WSL2). Backend and DB run in Docker Compose. Mobile runs via Expo Go on iPhone and Android emulator. |
| CI | GitHub Actions |

## 4. Repository layout

```
/
├── CLAUDE.md
├── README.md
├── api/openapi.yaml              # the contract
├── backend/
│   ├── cmd/server/               # main: wires everything
│   ├── internal/
│   │   ├── config/               # env config
│   │   ├── platform/             # db pool, logging, metrics, http server
│   │   ├── layer/                # generic Provider/Feature types + registry
│   │   ├── transit/
│   │   │   ├── static/           # GTFS download + import + derived tables
│   │   │   ├── realtime/         # GTFS-RT poller + parser
│   │   │   ├── departures/       # schedule window + RT merge → Snapshot
│   │   │   └── provider.go       # transit implementation of layer.Provider
│   │   ├── geo/                  # geohash cells, scope (Berlin) filter
│   │   └── api/                  # handlers, cache, etag (uses generated types)
│   ├── db/
│   │   ├── migrations/
│   │   └── queries/              # sqlc input
│   ├── gen/                      # generated code (oapi-codegen, sqlc) — do not edit
│   └── testdata/                 # small recorded fixtures
├── mobile/
│   ├── app/                      # Expo Router screens
│   └── src/
│       ├── core/                 # pure TS domain logic (no RN imports)
│       ├── api/                  # generated types + client
│       ├── layers/               # layer registry + transit layer UI
│       ├── state/                # zustand stores
│       ├── ui/                   # design tokens + shared components
│       └── lib/                  # storage, location, platform glue
├── docs/
│   ├── PLAN.md  ARCHITECTURE.md  DATA_MODEL.md  PROGRESS.md
│   └── adr/
├── docker-compose.yml
└── .github/workflows/
```

## 5. Commands

> Every command runs **from the repo root** in Windows PowerShell 5.1 or PowerShell 7 (no `cd` needed; `go -C` / `npm --prefix` pick the directory). Keep this section accurate when commands change.
>
> Needs Docker Desktop running for: Compose, backend integration tests (testcontainers), lint, `go generate` (sqlc runs in a container).

```powershell
# --- backend + database (Docker Compose) ---
docker compose up -d --build --wait          # build + start db and backend, wait until healthy
curl.exe http://localhost:8080/v1/health     # curl.exe, not curl: in PowerShell 5.1 `curl` is Invoke-WebRequest
docker compose logs -f backend               # follow backend logs (Ctrl+C stops following)
docker compose down                          # stop; data stays in volume livestop_pgdata (`down -v` deletes it)

# --- backend checks ---
go -C backend vet ./...
go -C backend test ./...                     # unit + integration tests (starts PostGIS containers)
go -C backend test -short ./...              # unit tests only, no Docker
docker run --rm -v "${PWD}\backend:/app" -v golangci-cache:/root/.cache -w /app golangci/golangci-lint:v2.15.0 golangci-lint run ./...
                                             # lint: same version as CI, config backend/.golangci.yml (incl. gofmt)

# --- code generation (after changing api/openapi.yaml or backend/db/queries) ---
go -C backend generate ./...                 # oapi-codegen (go tool) + sqlc (pinned Docker image)
npm --prefix mobile run gen:api              # TypeScript types → mobile/src/api/schema.gen.ts
                                             # commit the generated files; CI fails on drift

# --- mobile ---
npm --prefix mobile ci                       # install exact versions from package-lock.json
npm --prefix mobile run typecheck
npm --prefix mobile run lint                 # ESLint + Prettier check (`npm --prefix mobile run format` fixes formatting)
npm --prefix mobile test                     # Vitest (src/core, tests/node) + Jest (UI)
npm --prefix mobile start                    # Expo dev server: scan QR with Expo Go (iPhone) or press "a" (Android emulator)
npm --prefix mobile start -- --clear         # after npm install or a mobile/.env change (Metro cache)
cd mobile; npx expo install <package>; cd .. # add an Expo/RN dependency with the SDK-matched version

# --- dev-only (Phase 1.7, not implemented yet) ---
curl.exe -X POST http://localhost:8080/internal/import   # trigger a GTFS import; disabled unless LIVESTOP_DEV=true
```

Notes:
- Phone setup (LAN IP, `mobile/.env`, firewall, Expo login): README "Run the app on your phone".
- `go test -race` needs cgo (not available on the Windows dev machine); CI runs it.
- After `npm install`, restart a running Expo server with `--clear`, otherwise Metro fails with "Unable to resolve module".

## 6. Definition of done (every task)

- [ ] Code compiles, linters pass.
- [ ] Tests for the new behaviour exist and pass (unit; integration where DB/feeds are involved).
- [ ] Generated code is up to date (CI checks for drift).
- [ ] Acceptance criteria of the task in PLAN.md are met.
- [ ] `docs/PROGRESS.md` updated (task status + session log entry).
- [ ] If a data fact was discovered → `docs/DATA_MODEL.md` §7 updated.
- [ ] If a decision was made → ADR written (or existing one referenced).
