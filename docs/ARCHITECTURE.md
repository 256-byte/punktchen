# Architecture

## 1. Goals

1. Show, on a map, the next departures of every stop pole within ~500 m of the user, per direction, live.
2. **Upstream traffic must not grow with users.** One poller per data source, shared by everyone.
3. **The app keeps working with bad or no connectivity** (last known data + local countdown).
4. **Generic data layers.** Transit is the first layer; public toilets, elevator status, disruption alerts etc. can be added later without touching the map or the core.
5. Clean, testable, observable backend — this is the portfolio showcase.

Non-goals (for now): journey planning, ticketing, user accounts, push notifications, publishing to the stores.

## 2. System overview

```
                   ┌───────────────────────── backend (single Go binary) ─────────────────────────┐
VBB GTFS zip  ───► │ transit/static   ── import (daily) ──►  PostgreSQL + PostGIS                  │
 (daily)           │                                          (schema "gtfs" + derived tables)    │
                   │                                                   │                           │
                   │ transit/departures ◄── schedule window (every 60 s) ┘                          │
VBB GTFS-RT   ───► │ transit/realtime ── TripUpdates (every 15 s, ETag) ──► merge                   │
 (7 req/min)       │                                                         │                      │
                   │                                      Snapshot (immutable, atomic.Pointer)      │
                   │                                                         │                      │
                   │ layer registry ── transit Provider ─────────────────────┘                      │
                   │        │                                                                       │
                   │ api: /v1/nearby ── cell cache (geohash + snapshot version) ── ETag / 304       │
                   └────────┼──────────────────────────────────────────────────────────────────────┘
                            │ HTTPS/JSON (OpenAPI contract)
                   ┌────────▼─────────┐
                   │  Mobile (Expo)   │  TanStack Query + persisted cache, local countdown,
                   │                  │  layer registry → markers + detail sheets on the map
                   └──────────────────┘
```

All backend parts live in **one binary** (modular monolith, ADR-0002). Package boundaries are strict so parts could be split later.

## 3. Backend components

### 3.1 `transit/static` — GTFS schedule import

- **Source:** current VBB GTFS (stop-pole precision), URL in config (`LIVESTOP_GTFS_URL`). Default: `https://unternehmen.vbb.de/gtfs`, the link on VBB's open-data page (https://unternehmen.vbb.de/digitale-services/datensaetze/, "Aktualisierung: 2x wöchentlich"; `https://www.vbb.de/gtfs` serves the same file). It is the feed the GTFS-RT trip ids refer to; the older "gtfs-mastscharf" file is not kept up to date (DATA_MODEL §7 #15).
- **Size:** ~85 MB zipped, ~650 MB unzipped. `stop_times.txt` dominates (~5.8 M rows).
- **Schedule:** once a day (default 03:30 Europe/Berlin) + on startup if no data.
- **Change detection:** conditional GET with `ETag` / `Last-Modified` when the server sends them; the default URL sends neither, so the zip is downloaded and its **SHA-256** compared with the last successful import in `meta.feed_imports`. Unchanged → `skipped`.
- **Import (ADR-0004):**
  1. Stream zip → CSV readers (no full extraction to RAM).
  2. Load into schema `gtfs_staging` using `pgx.CopyFrom`.
  3. Create indexes, run derived-table SQL (see DATA_MODEL §3).
  4. Apply scope filter (Berlin bbox, config) by flagging `stops.in_scope` — rows are kept, filtering happens in queries/derived tables.
  5. Sanity checks (`static.check`, all run, all problems reported together): in-scope poles and stop_times above floors (VBB defaults 5,000 / 1,000,000) and not more than 50 % below the last successful import; no dangling `trip_id` / `stop_id` / `route_id`; every trip has a running service and stop times; served poles have coordinates; every stop time has a departure; the feed has service in `[today, today + 7]`. Fail → keep old data, attempt `failed` with the list; `gtfs_staging` stays for inspection until the next load drops it.
  6. Swap, one transaction with `lock_timeout` 10 s: `gtfs` → `gtfs_old`, `gtfs_staging` → `gtfs`; commit (~20 ms); then drop `gtfs_old` in its own transaction (`lock_timeout` 60 s; on timeout the next swap drops it).
- Readers never see a half-imported dataset. sqlc queries always reference `gtfs.*`. A statement resolves `gtfs.<table>` either before the swap commits (old tables) or after (new ones); statements still running on the old tables keep their locks, so the drop waits for them. Verified with 4 concurrent readers over 6 swaps: ~4,000–4,500 reads, none failed (task 1.6).
- `static.Importer.Run` is the whole pipeline (download → load → check → swap → `meta.feed_imports`). A Postgres advisory lock (`pg_try_advisory_lock`) makes concurrent runs (scheduler + manual trigger, several processes) return `ErrImportRunning` instead of racing.

### 3.2 `transit/departures` — schedule window

- Every 60 s, load all scheduled departures for **in-scope stops** in `[now - 2 min, now + 90 min]` from Postgres.
- Must handle: service days (`calendar` + `calendar_dates`), times ≥ 24:00:00 (trip belongs to the previous service day), DST (GTFS times are relative to "noon minus 12h" of the service day).
- Result: `map[stopID][]ScheduledDeparture` sorted by time.

### 3.3 `transit/realtime` — GTFS-RT ingestor

- **Source:** `https://production.gtfsrt.vbb.de/data` (CC BY 4.0, no auth, **max 7 req/min** since 2026-11-02, ETag supported). Config: `LIVESTOP_GTFSRT_URL`. Terms on https://production.gtfsrt.vbb.de/ (DATA_MODEL §7 #20).
- Poll every 15 s (config, floor 10 s) with `If-None-Match`. 304 → nothing to do.
- Request compressed data (`Accept-Encoding: gzip`; ~5× smaller) and send a User-Agent that names the project and its repository URL.
- Parse protobuf `FeedMessage`; the feed contains **TripUpdates only** (no vehicle positions, no alerts).
- Keep latest state per trip instance `(trip_id, start_date)`.
- The VBB feed matches VDV data to GTFS itself; ambiguous matches are dropped upstream → **not every trip has realtime** (BVG U-Bahn, tram and bus coverage is low, DATA_MODEL §7 #19). Schedule fallback is mandatory, not optional.
- **Freshness comes from `header.timestamp`, not from HTTP success.** The feed sometimes serves an empty FullDataset with a days-old header (DATA_MODEL §7 #14). Such a response is "no data": keep the last good state until it is too old, never treat it as "no delays".
- Errors/timeouts: exponential backoff (max 2 min), keep last good state, expose `feed_age_seconds` (= now − `header.timestamp` of the last applied feed).

### 3.4 Merge → `Snapshot`

Every time either the schedule window or the RT state changes:

1. Start from scheduled departures.
2. For each trip with a TripUpdate:
   - trip `CANCELED` → mark all its departures cancelled.
   - per stop: use `stop_time_update` matched by `stop_id` (fallback `stop_sequence`).
   - stops **after** the last update without own data → propagate the last known delay (GTFS-RT semantics).
   - stops **before** the first update → keep schedule.
   - `SKIPPED` stop → remove that departure.
3. Set `source = realtime | scheduled` per departure. VBB fills events without realtime data from the schedule (`time` without `delay`), so only events with `delay` count as `realtime` (DATA_MODEL §7 #18). `stop_id`s that are bare station numbers match by `stop_sequence`.
4. Build an immutable `Snapshot{Version, GeneratedAt, RealtimeFeedTime, ByStop}` and publish with `atomic.Pointer[Snapshot]` (ADR-0005). Readers are lock-free.
5. `Version` increments monotonically; it is part of cache keys and ETags.

### 3.5 `layer` — generic provider system (ADR-0008)

```go
type Provider interface {
    Layer() string                                         // "transit", later "toilets", "alerts", ...
    Nearby(ctx context.Context, q NearbyQuery) (LayerResult, error)
    Version() uint64                                       // changes when data changes → cache invalidation
    Health() ProviderHealth                                // freshness, last error
}
```

- The API asks the registry for the requested layers and composes the response.
- Each layer has its **own typed payload** in OpenAPI (discriminated by `layer`).
- Static layers (e.g. toilets) refresh rarely; live layers (transit) refresh in seconds. The cache handles both through `Version()`.
- Only `transit` is implemented until a later phase says otherwise.

### 3.6 `api` — HTTP

- Go `net/http` with method+path patterns; handlers implement interfaces generated by `oapi-codegen` from `api/openapi.yaml`.
- Endpoints: see DATA_MODEL §5.
- **Cell cache (ADR-0006):**
  - Snap request location to a geohash cell (precision 7, ~150 m × 150 m).
  - Query around the **cell center** with `radius + cell half-diagonal`; all users in the same cell share one response.
  - Cache key: `(layers, cell, radiusBucket, providerVersions)`. New snapshot → new key → old entries just expire (LRU + TTL safety net). No explicit invalidation code.
  - `ETag` = hash of the key. `If-None-Match` match → `304` with empty body.
  - Response bodies gzip-compressed.
- Middleware: request ID, structured access log, panic recovery, timeout, CORS (dev only).
- `/metrics` on a separate port (not exposed publicly).

### 3.7 Observability

- Logs: `log/slog` JSON, one line per request, plus component logs with `component=` attribute.
- Metrics (Prometheus): `livestop_gtfsrt_feed_age_seconds`, `livestop_gtfsrt_poll_total{result}`, `livestop_rt_match_ratio`, `livestop_snapshot_build_seconds`, `livestop_snapshot_version`, `livestop_import_duration_seconds`, `livestop_import_last_success_timestamp`, `livestop_http_request_duration_seconds{route,status}`, `livestop_cache_hits_total{result}`.
- `/v1/health`: `ok` | `degraded` (RT feed older than 2 min by `header.timestamp`, or schedule older than 3 days) | `down` (no schedule data).
  - Always HTTP 200 while the process runs; the body carries the status. The Docker healthcheck (`livestop healthcheck`) is therefore a pure liveness check and does not restart the container during e.g. a long first import.
  - Phase 0: `down` if the database is unreachable, else `ok`. Freshness rules are added with the importer (Phase 1) and the realtime poller (Phase 4).

## 4. Mobile architecture

### 4.1 Data flow

```
expo-location ──► location store (zustand) ──► fetch policy (core) ──► TanStack Query
                                                                         │  GET /v1/nearby (If-None-Match)
                                                                         ▼
                                                    persisted query cache (AsyncStorage)
                                                                         │
                       global 1 s ticker ──► countdown (core) ──► layer markers + sheets
```

### 4.2 Fetch policy (pure function in `src/core/fetchPolicy.ts`)

Fetch only when the app is in the foreground **and** at least one is true:
- moved > 150 m since last successful fetch,
- the earliest shown departure has passed,
- data age > 30 s,
- user pulled to refresh.

Never more often than every 10 s. On error: backoff 5 s → 10 s → 20 s → 60 s (cap). On `304`: keep data, update "last checked" time.

### 4.3 Offline / staleness (pure functions in `src/core/staleness.ts`)

- On app start, render the persisted response immediately (no spinner), then refetch.
- Departures in the past are dropped locally.
- Data age < 60 s: normal. 60 s – 5 min: show "updated X ago". > 5 min: dimmed realtime values, show scheduled time emphasis. No network: banner "offline — showing last known data".

### 4.4 Layer registry (mobile)

```ts
interface MapLayer<TFeature> {
  id: LayerId;                                  // "transit"
  select(response: NearbyResponse): TFeature[]; // pick this layer's features
  Marker: React.ComponentType<{ feature: TFeature; now: number }>;
  Sheet: React.ComponentType<{ feature: TFeature; now: number }>;
  refresh: { minIntervalMs: number };
}
```

The map screen iterates over enabled layers; it does not know what a "departure" is.

### 4.5 Map performance rules

- Re-render markers only when the **displayed minute** changes, not every second.
- `react-native-maps` custom markers: keep `tracksViewChanges` false except right after content changes.
- Cluster / hide poles at low zoom; show at most next departure per direction on the marker; the rest in the sheet.

### 4.6 Tooling and tests

- **Vitest** (plain Node) runs `src/core/**/*.test.ts` and Node-only tooling tests in `tests/node/` (e.g. the ESLint import-boundary test).
- **Jest + jest-expo + React Native Testing Library** run everything that renders React Native (`tests/ui/`, and later `src/{layers,ui,...}/**/*.test.tsx`).
- Test files never live in `app/`: every file there is an Expo Router route.
- `src/api/schema.gen.ts` is generated by `npm run gen:api` (openapi-typescript) from `api/openapi.yaml`; never edited, ignored by ESLint and Prettier.
- Dependencies with native code are added with `npx expo install` (SDK-matched versions). Expo Go only contains its bundled native modules.

## 5. Environments

| Env | Where | Notes |
|---|---|---|
| dev | Windows PC, Docker Desktop | Phone reaches backend via PC LAN IP (same Wi-Fi). Windows Firewall rule for port 8080. |
| server (later) | any Linux host | Same `docker-compose.yml`. Decided later. |

## 6. Attribution & licences

- VBB GTFS and GTFS-RT: CC BY 4.0 → show "Data: VBB (CC BY 4.0)" in the app (map overlay/about) and in API `meta.attribution`.
- Map: Apple Maps (iOS) / Google Maps (Android) via `react-native-maps`. No cloud map IDs (keeps Google Maps native usage free).
