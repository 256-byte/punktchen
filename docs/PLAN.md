# Plan

> Phases are done **in order**. A phase is complete only when all its acceptance criteria pass.
> Task IDs (e.g. `1.3`) are referenced in `docs/PROGRESS.md` and commit messages (`[1.3] import stop_times`).

## Phase 0 — Skeleton & tooling

Goal: an empty but complete, runnable, tested project.

| ID | Task | Acceptance criteria |
|---|---|---|
| 0.1 | Repo layout as in CLAUDE.md §4, `.gitignore` (data dirs, `.env`, node_modules, build output), `.editorconfig`, MIT `LICENSE`, short `README.md` (what/why/how to run) | Tree matches CLAUDE.md §4 |
| 0.2 | `docker-compose.yml`: `db` (postgis/postgis, current stable major, volume, healthcheck) + `backend` (multi-stage Dockerfile, distroless/alpine runtime). `.env.example` with all `LIVESTOP_*` vars | `docker compose up -d` → both healthy |
| 0.3 | Go module, `cmd/server`, `internal/config` (env parsing + validation, fails fast), `internal/platform` (slog JSON logger, pgx pool, graceful shutdown on SIGTERM, metrics server) | Server starts, logs config summary (no secrets), shuts down cleanly |
| 0.4 | `golang-migrate` wired; migration `0001` creates `meta` schema + `feed_imports`; enable PostGIS extension | Migrations run on startup; idempotent |
| 0.5 | `api/openapi.yaml` v0: `/v1/health`, `/v1/layers`, `/v1/nearby`, `/v1/stations/{id}` with schemas from DATA_MODEL §5. `oapi-codegen` via `go generate`; `/v1/health` implemented, others return 501 | `curl localhost:8080/v1/health` → 200 JSON |
| 0.6 | `sqlc` config + one trivial query; `go generate ./...` runs both generators | Generation is reproducible |
| 0.7 | Expo app (latest SDK, TypeScript strict, Expo Router) in `mobile/`; ESLint (with rule: `src/core` must not import react/react-native/expo), Prettier, Vitest for `src/core`, jest-expo for components; `npm run gen:api` with openapi-typescript | `npm run typecheck && npm run lint && npm test` pass; app opens in Expo Go on iPhone |
| 0.8 | API base URL config for mobile (`EXPO_PUBLIC_API_URL`), doc in README: find PC LAN IP, Windows Firewall inbound rule for 8080, phone on same Wi-Fi | App shows `/v1/health` result on screen on the real iPhone |
| 0.9 | GitHub Actions: `backend` (go vet, golangci-lint, go test with Docker), `mobile` (typecheck, lint, test), `contract` (regenerate → fail on git diff) | All green on a PR |
| 0.10 | Fill CLAUDE.md §5 commands with the real ones | Commands copy-paste work on Windows PowerShell |

## Phase 1 — Static GTFS

Goal: a correct, swappable schedule database with derived tables.

| ID | Task | Acceptance criteria |
|---|---|---|
| 1.1 | **Data exploration.** Script/test (not shipped) that downloads the feed and prints: file sizes/row counts, sample stop IDs (station/pole), route_type histogram, direction_id null ratio, max departure_time, ring line headsigns, platform_code coverage. Also record ~5 GTFS-RT samples (15 s apart) into `backend/testdata/` (if < 5 MB, else a trimmed version). | Findings written to DATA_MODEL §7, all **(verify)** items answered |
| 1.2 | Downloader: conditional GET when the server sends `ETag`/`Last-Modified`, otherwise download + SHA-256 compare (the default URL sends neither, DATA_MODEL §7 #15); writes `meta.feed_imports` | Unchanged file → status `skipped`, no import started. Tests with a fake HTTP server cover both paths (304, and same SHA-256 without validators) |
| 1.3 | Streaming CSV → `gtfs_staging` via `CopyFrom`; times parsed to seconds; geometry column | Full import finishes; row counts logged; memory stays < 500 MB |
| 1.4 | Indexes + `in_scope` flag from bbox config | Count of in-scope poles and stations (grouped as in DATA_MODEL §3.4) plausible (Berlin) and documented in §7 |
| 1.5 | Derived tables: `stations` (grouping by `parent_station`, else DHID prefix; name, centroid, modes), `service_dates`, `pole_directions` (terminal/origin logic, loop trips, `is_ring`). Plus a real-data static subset fixture around U Wedding (cut with `gtfsexplore`, < 5 MB) that matches the GTFS-RT fixtures | Tests on a tiny hand-written fixture GTFS cover: weekday service, added/removed dates, trip after 24:00, terminal detection, loop trip not terminal at its origin, pole without `parent_station` grouped by DHID prefix. On the Wedding subset: bus poles `::3`–`::8` belong to station `de:11000:900009104` |
| 1.6 | Sanity checks + atomic schema swap (ADR-0004) | Integration test: failed import leaves old data readable; successful import swaps with zero failed reads during swap |
| 1.7 | Scheduler: daily import at configured time + import on empty DB at startup; dev endpoint `POST /internal/import` | Manual trigger works; logs + metrics emitted |
| 1.8 | sqlc query: poles within radius of point (PostGIS `ST_DWithin`), grouped by station | "Wedding 500 m" (52.5427, 13.3660) returns S+U Wedding as **one** station including its bus poles, and S Wedding/Martha-Ndumbe-Platz (`de:11000:900008103`) as a separate station; < 20 ms |

## Phase 2 — Schedule-based API

Goal: real `/v1/nearby` from the schedule only. API is layer-based from day one.

| ID | Task | Acceptance criteria |
|---|---|---|
| 2.1 | `internal/layer`: `Provider` interface, registry, `NearbyQuery`, composition of multi-layer responses | Unit tests with a fake provider |
| 2.2 | `transit/departures`: schedule window loader (service dates, ≥ 24:00 handling up to 32 h on the previous service day, DST-safe) | Tests: trip at 24:30 on service day D appears on calendar day D+1 00:30; trip at 31:17 on D appears on D+1 07:17; DST switch day fixture |
| 2.3 | Snapshot type + builder (schedule only for now) + `atomic.Pointer` publish + version counter | Race detector clean under concurrent reads |
| 2.4 | Transit provider: build features (station → poles → directions → next 3 departures), terminal rule, mode mapping from `route_type` (rail replacement buses stay `bus`), colours: `route_color`, else VBB line colour list (`linienfarben`), else null | Golden JSON test for the Wedding fixture area |
| 2.5 | `/v1/nearby`, `/v1/stations/{id}`, `/v1/layers` handlers; validation; error format | Contract tests: responses validate against openapi.yaml |
| 2.6 | Spot check vs. official VBB departures for 5 stops (manual, documented in PROGRESS) | Scheduled times match |

## Phase 3 — Mobile MVP

Goal: see live-counting pins around you on the iPhone.

| ID | Task | Acceptance criteria |
|---|---|---|
| 3.1 | `src/core`: time/countdown/labels, effective departure, staleness, fetch policy (pure functions) | Vitest coverage ≥ 90% for `src/core` |
| 3.2 | API client (openapi-fetch) + TanStack Query hook `useNearby` with ETag/`If-None-Match` | 304 keeps previous data (test with mocked fetch) |
| 3.3 | Location: permission flow, last known position, movement threshold | Denied permission → friendly screen with "use map center" fallback |
| 3.4 | Map screen: `react-native-maps`, follows user on open, layer registry (§4.4 ARCHITECTURE) | Map centers on user on app start |
| 3.5 | Transit layer: pole markers with next departure per direction (line badge + headsign + countdown), global 1 s ticker, minute-level re-render | Markers update without visible jank on iPhone 15 |
| 3.6 | Station sheet (`@gorhom/bottom-sheet`) with all poles/directions/next departures | Tap marker → sheet; swipe to dismiss |
| 3.7 | Attribution "Data: VBB (CC BY 4.0)" visible on map | Visible |

## Phase 4 — Realtime

| ID | Task | Acceptance criteria |
|---|---|---|
| 4.0 | **Realtime coverage check.** Weekday daytime samples (morning, noon, evening) with `gtfsexplore coverage`: share of trips per mode that have real `delay` values (DATA_MODEL §7 #18, #19). Decide whether an additional realtime source is needed for BVG U-Bahn/tram (if yes: ADR first, user approval) | Numbers in DATA_MODEL §7; decision in PROGRESS |
| 4.1 | GTFS-RT poller: interval, `If-None-Match`, gzip, timeout, backoff, metrics; never > 7 req/min (provider limit since 2026-11-02); User-Agent with project name + repository URL | Unit test with fake server: 304 path, error/backoff path |
| 4.2 | Parser → trip-instance state (latest wins), using recorded testdata; an empty feed with a stale header is "no data" and keeps the last state | Golden tests on recorded feeds incl. `vbb-empty-stale.pb` |
| 4.3 | Merge rules (ARCHITECTURE §3.4): stop match (`stop_id`, else `stop_sequence` for bare station numbers), delay propagation, cancellations, skipped stops, `source` flag (`realtime` only for events with `delay`) | Table-driven tests for each rule |
| 4.4 | Rebuild snapshot on RT change; `realtimeAgeSeconds` from `header.timestamp`; health `degraded` when the feed is > 2 min old | Killing upstream (fake) → degraded, schedule still served; stale empty feed → degraded |
| 4.5 | Mobile: show delay (e.g. +2), realtime vs scheduled indicator, cancelled style | Visible on device |
| 4.6 | Metric `livestop_rt_match_ratio` + note in PROGRESS of observed ratio | Ratio observed and documented |

## Phase 5 — Traffic & cache

| ID | Task | Acceptance criteria |
|---|---|---|
| 5.1 | Geohash cell snapping + radius expansion (ADR-0006) | Unit tests: users 50 m apart in same cell get identical response |
| 5.2 | LRU cache keyed by `(layers, cell, radiusBucket, providerVersions)`, TTL safety net, metrics | Load test: 1000 req for one cell → 1 build |
| 5.3 | ETag/304 end-to-end, gzip | `curl` with `If-None-Match` → 304 |
| 5.4 | Mobile fetch policy wired (foreground, 150 m, earliest passed, 30 s age, 10 s min, backoff) | Dev log shows request count per minute ≤ policy |
| 5.5 | Document numbers (requests/min client, cache hit ratio) in README "Performance" section | Present |

## Phase 6 — Offline & resilience

| ID | Task | Acceptance criteria |
|---|---|---|
| 6.1 | Persist query cache (AsyncStorage persister), render persisted data instantly | Cold start shows last data without spinner |
| 6.2 | Staleness UI ("updated X ago", dimming, offline banner) | Airplane mode test documented |
| 6.3 | Drop past departures locally; keep countdown running | Correct after 10 min offline |

## Phase 7 — UI polish

| ID | Task | Acceptance criteria |
|---|---|---|
| 7.1 | Design tokens (colors, type, spacing), light/dark mode | Consistent in both modes |
| 7.2 | Marker clustering / zoom-based detail level | Usable at Alexanderplatz density |
| 7.3 | Ring lines ⟳/⟲, terminal display (only return direction) | Correct at Wedding (S41/S42) and a terminal stop |
| 7.4 | 3D pitch toggle, buildings | Works on iOS and Android |
| 7.5 | Empty/error states, haptics, accessibility labels | VoiceOver reads marker content |
| 7.6 | README screenshots/GIF, architecture diagram | Portfolio-ready README |

## Later (not scheduled — needs user approval + ADR before starting)

- **Phase 8 — Alerts layer:** research VBB Verkehrsmeldungen open data (format/terms) → first adapter → normalized `Alert` → de-dup across sources (HAFAS remarks, DB RIS) → ⚠︎ in UI.
- Elevator status layer (BVG, DB FaSta).
- Public toilets layer (Berlin open data).
- SSE `/v1/stream` for pushing only changes.
- Widgets & Live Activities (`expo-widgets`, needs development build).
- Server deployment (same compose file), TLS.
- Germany-wide data source behind the Provider interface.
- Redis for multi-instance cache.
