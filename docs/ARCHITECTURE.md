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
- **Schedule** (`static.Scheduler`, task 1.7): daily at `LIVESTOP_GTFS_IMPORT_TIME` (default 03:30 Europe/Berlin, wall clock kept across DST), at startup if there is no current live schema (none, or another layout, see below), 1 h after a failed run (never later than the next daily run), and on `POST /internal/import` (dev only, answers 202 at once; 409 while running). A run blocked by the advisory lock counts as `busy`, not as a failure.
- **Change detection:** conditional GET with `ETag` / `Last-Modified` when the server sends them; the default URL sends neither, so the zip is downloaded and its **SHA-256** compared with the last successful import in `meta.feed_imports`. Unchanged → `skipped`.
- **Layout stamp:** every import writes `COMMENT ON SCHEMA gtfs_staging IS 'livestop layout <id>'`; the comment moves with the swap. `<id>` = `static.Layout`, a hash of `db/gtfs/{schema,indexes,derived}.sql` and the Go constant `layoutRevision`. If the live schema is missing or has another stamp, the scheduler imports at startup and the downloader ignores `ETag` / SHA-256 (`Fetch(ctx, force=true)`): a new binary with changed gtfs tables gets them without waiting for a new feed. SQL changes are picked up automatically; Go changes to what an import writes (e.g. a column transform in `tables.go`) must increase `layoutRevision`.
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

- Every 60 s, the snapshot builder (task 2.3, `departures.Builder`) moves its window to `[now − 30 min, now + 90 min)` (30 min back since task 4.4, so a late vehicle stays until it leaves; readers drop departures by predicted time) (scheduled departures at **in-scope stops**). It drops the departures that fell out at the start and loads only the new part at the end (~1 min, ~70 ms on the full feed). The whole window is loaded at startup, after an import (the importer calls `Reload`; a changed `ScheduleID` on the new part triggers the same), and when the clock jumped. A failed refresh keeps the last snapshot.
- Must handle: service days (`calendar` + `calendar_dates`), times ≥ 24:00:00 (trip belongs to the previous service day), DST (GTFS times are relative to "noon minus 12h" of the service day).
- Result: departures grouped by pole (`Snapshot.Departures(stopID)`), each list sorted by time.
- `departures.Loader.Load(ctx, from, to)` (task 2.2) loads any half-open window `[from, to)`; adjacent windows never share a departure, so the refresh can load just the new minute instead of the whole window.
  - Go splits the window into one seconds range per service day that can reach it: service day start = noon − 12 h in Europe/Berlin, looking back as many days as the feed's largest stop time needs (`max(departure_secs)`, VBB 31:17:00 → yesterday).
  - One SQL query per service day (`ScheduledDepartures`): `stop_times` by `departure_secs` index, joined with `trip_info` and `service_dates` of that day, in-scope poles only. Left out: the trip's last stop (arrival only) and `pickup_type = 1`.
  - Run with `plan_cache_mode = force_custom_plan` in a read-only transaction: a cached generic plan cannot see the range width and was 5–10× slower.
  - Consistency: the OID of the `gtfs` schema is read before and after; an import swap in between → load again (max 3 tries). The OID is returned as `ScheduleID`, so callers know when the schedule changed.
  - Full feed, dev PC: see PROGRESS 2.2 for timings.

### 3.3 `transit/realtime` — GTFS-RT ingestor

- **Source:** `https://production.gtfsrt.vbb.de/data` (CC BY 4.0, no auth, **max 7 req/min** since 2026-11-02, ETag supported). Config: `LIVESTOP_GTFSRT_URL`. Terms on https://production.gtfsrt.vbb.de/ (DATA_MODEL §7 #20).
- Poll every 15 s (config, floor 10 s) with `If-None-Match`. 304 → nothing to do.
- Request compressed data (`Accept-Encoding: gzip`; ~5× smaller) and send a User-Agent that names the project and its repository URL.
- Parse protobuf `FeedMessage`; the feed contains **TripUpdates only** (no vehicle positions, no alerts).
- Keep latest state per trip instance `(trip_id, start_date)`.
- The VBB feed matches VDV data to GTFS itself; ambiguous matches are dropped upstream → **not every trip has realtime**: about 85 % of the trips running at a given moment carry a real delay (U-Bahn ~100 % in the daytime, tram and bus 85–92 %, S-Bahn ~80 %, regional ~67 %, ferry none; measured on four days and times, DATA_MODEL §7 #19). Schedule fallback is mandatory, not optional.
- **Freshness comes from `header.timestamp`, not from HTTP success.** The feed sometimes serves an empty FullDataset with a days-old header (DATA_MODEL §7 #14). Such a response is "no data": keep the last good state until it is too old, never treat it as "no delays".
- Errors/timeouts: exponential backoff (max 2 min), keep last good state, expose `feed_age_seconds` (= now − `header.timestamp` of the last applied feed).
- **State** (task 4.2, `realtime.Parse`, `realtime.Store`): a feed becomes an immutable `State` keyed by trip instance `(trip_id, start_date)` — one feed carries today's and tomorrow's trips (DATA_MODEL §7 #29). Kept per stop update: stop_sequence (or none), stop_id, SKIPPED / NO_DATA, arrival and departure time + delay (realtime only with a delay). Within a feed the latest update of a trip instance wins (TripUpdate.timestamp). Only SCHEDULED and CANCELED trips (VBB sends nothing else); DIFFERENTIAL feeds and feeds without header.timestamp are errors. `Store.Apply` is the poller's handler: a feed with no trip updates is "no data" and keeps the last state (§7 #14); a feed whose header.timestamp is not newer than the state is ignored ("latest wins" across feeds); a feed that cannot be parsed is an error (the poller then fetches it again). Readers use `Store.Current()` without locks (`atomic.Pointer`, as for the snapshot).
- **Poller** (task 4.1, `realtime.Poller`): `Run` polls every `LIVESTOP_GTFSRT_POLL_INTERVAL` (15 s); each `Poll` first waits until 10 s (`MinRequestGap`, 6/min) after the last request or until a `Retry-After` (429/503) has passed, whatever triggered it. `If-None-Match` with the last ETag; gzip via net/http (it asks and decompresses itself); 20 s timeout per request; feeds over 64 MB are refused. A 200 goes to a `Handler(ctx, Feed{Body, ETag, FetchedAt})` unparsed (parsing: 4.2); if the handler fails, the ETag is dropped so the next poll fetches the feed again. Failed polls (error, other status, rejected) wait 15 s → 30 s → 1 min → 2 min (cap); any success resets. Metric `livestop_gtfsrt_poll_total{result=updated|not_modified|rejected|error}`. net/http replays a GET once when a reused keep-alive connection closes before the answer, so a poll can rarely send two requests; 6/min leaves room under 7. Started by the server since task 4.4 (`cmd/server`: poller → `Store.Apply`; the store calls `Builder.RealtimeChanged` after each applied feed).

### 3.4 Merge → `Snapshot`

Every time either the schedule window or the RT state changes:

1. Start from scheduled departures.
2. For each trip instance `(trip_id, start_date)` with a TripUpdate (`realtime.Merge`, task 4.3):
   - trip `CANCELED` → all its departures `Cancelled` (no delay).
   - per stop: the stop time update with the departure's `stop_sequence`; without a sequence, the one whose `stop_id` is the pole (only if that stop_id occurs once in the trip). Sequence first because loops pass a pole twice; on the recorded Wedding feed every sequence match also matches the pole or its station number (DATA_MODEL §7 #30).
   - its delay: departure event, else arrival (a first stop may arrive after it departs, §7 #29). Only an event with a `delay` is realtime; VBB fills the others from the schedule (§7 #18). `NO_DATA` → schedule.
   - `SKIPPED` stop → remove that departure.
   - stops without an own update → the last update before it (by sequence, skipped ones passed over) decides: with a delay → propagate it (GTFS-RT semantics); filled from the schedule or `NO_DATA` → schedule. VBB lists every stop, so propagation did not occur on the recorded feed.
   - stops before the first update → `TripUpdate.delay` if set, else schedule.
3. `Departure.Realtime` (and `Delay`, `Predicted()`) is set for departures with a delay; the API's `source = realtime | scheduled` follows from it. `Merge` returns a new slice and `MergeStats` (with a TripUpdate, realtime, propagated, cancelled, skipped) for logs. `MatchRatio` (task 4.6): the share of departures scheduled within ±15 min of now (`MatchWindow`) whose trip instance has a TripUpdate; `Store.Merger` sets it as `livestop_rt_match_ratio` after every merge (0 when nothing is merged). Only near-now departures count because VBB sends TripUpdates for running trips only (DATA_MODEL §7 #31): normal is ~0.93; a sharp drop means static and realtime IDs stopped matching. It lives in `transit/realtime` because that package already depends on `departures` (for `ServiceDay`); `departures` stays free of realtime.
4. Build an immutable `Snapshot{Version, GeneratedAt, ScheduleID, From, To, RealtimeFeedTime, departures by pole}` and publish with `atomic.Pointer[Snapshot]` (ADR-0005). Readers are lock-free. All departures sit in one array grouped by pole; each pole gets a capped slice of it, so a reader cannot change another pole by appending. Inside a pole departures are ordered by predicted time (a late vehicle can leave after the next one). Realtime is merged into every snapshot by the builder's `RealtimeFunc` (`Store.Merger`, task 4.4): the builder keeps the window schedule-only and merges at publish; after an applied feed (`RealtimeChanged`) it republishes the same window with the new state, without the database. A state older than 5 min (`realtime.DefaultMaxAge`) is not merged: old predictions shown as live mislead more than the schedule; the snapshot still reports its feed time.
5. `Version` increments monotonically; it is part of cache keys and ETags. It starts at the Unix time in milliseconds when the process starts, so it also keeps increasing across restarts and an ETag from before a restart cannot match new data. A new snapshot is published once a minute (the window moves) and after every applied realtime feed (every 15 s while the feed changes).

### 3.5 `layer` — generic provider system (ADR-0008)

```go
type Provider interface {
    Info() Info                                            // ID "transit" (later "toilets", ...), name, attribution
    Nearby(ctx context.Context, q NearbyQuery) (Result, error) // Result{Version, Payload}
    Version() uint64                                       // changes when data changes → cache invalidation
    Health() Health                                        // status, realtime age, last error (no I/O)
}
```

- `Registry` is built once at startup and read-only afterwards. `Resolve(ids)` removes duplicates, returns providers in registration order (stable composition and cache keys) and rejects unknown ids with `ErrUnknownLayer` (→ 400).
- `Registry.Nearby(ctx, q, ids)` asks the requested layers **concurrently** and returns a `Composite`: one `LayerResponse{Info, Version, Health, Payload, Err}` per layer plus the deduplicated attribution of the layers that returned data.
- A failing (or panicking) provider marks only its own layer `down` without payload; the other layers are still served. `Nearby` itself fails only for an invalid query, unknown layers or a cancelled context.
- `Result.Version` comes from the same data as the payload, so a response never pairs a payload with another state's version. `Provider.Version()` is for building cache keys before a response exists (ADR-0006).
- The package does not import the OpenAPI types: payloads are opaque (`any`) and the API maps each layer's payload to its typed field.
- Each layer has its **own typed payload** in OpenAPI (discriminated by `layer`).
- Static layers (e.g. toilets) refresh rarely; live layers (transit) refresh in seconds. The cache handles both through `Version()`.
- Only `transit` is implemented until a later phase says otherwise.
- **Transit provider** (`internal/transit`, task 2.4):
  - Per request: `NearbyPoles` (poles in the circle with their station) and `PoleDirections` (directions of those poles + route agency/colour), two indexed queries; departures come from the current snapshot, never from the DB. `Station(id)` does the same for all poles of one station (sheet, 10 departures per direction).
  - The payload is the generated contract type (`*openapi.TransitLayer`), so the golden test files are the API JSON.
  - `Result.Version` is the snapshot version, and departures start at the snapshot's `GeneratedAt`, so a response depends only on that version (cache/ETag safe). Static data is read from the live schema at request time; right after an import swap, the snapshot follows within seconds (`Reload`).
  - Which directions a pole lists (terminal, merge, trim) and colours: DATA_MODEL §3.2, §5.2.
  - Health: `down` without a snapshot, `degraded` when the snapshot is older than 3 refresh periods.

### 3.6 `api` — HTTP

- Go `net/http` with method+path patterns; handlers implement interfaces generated by `oapi-codegen` from `api/openapi.yaml`.
- Endpoints: see DATA_MODEL §5.
- Implemented (task 2.5, `internal/api/layers.go`):
  - `/v1/layers` lists the registry's layers; `/v1/nearby` goes through `layer.Registry.Nearby`; `/v1/stations/{id}` asks the transit provider directly (`TransitStations`). `cmd/server` builds the registry with the transit provider.
  - Validation the generated binding does not do: lat/lng ranges, radius 100–1000 (default 500), layer ids (default `transit`; unknown, empty or repeated → 400). All problems of a request are reported in one message.
  - A layer that fails (e.g. no departures before the first import) → still 200, layer `down` in `meta.layers`, no payload, not in `attribution`. Station sheet: 404 unknown, 503 `unavailable` without departures.
  - `meta.layers.<id>.version` = the version the payload was built from; `Cache-Control: no-cache`; ETag/304 and gzip since task 5.3 (below).
  - Contract tests (`contract_test.go`): every endpoint on the Wedding fixture and on an empty state, each response (status, headers, body) validated against `api/openapi.yaml` with kin-openapi (JSON Schema 2020-12 for OpenAPI 3.1).
- **Cell cache (ADR-0006):**
  - Snap request location to a geohash cell (precision 7, ~150 m × 150 m).
  - Query around the **cell center** with `radius + cell half-diagonal`; all users in the same cell share one response.
  - Done in task 5.1 (`internal/geo/cell.go`, `GetNearby`): a Berlin cell is ~153 m × 93 m, half diagonal ~90 m (largest centre–corner distance, rounded up + 1 m for sphere vs. spheroid), so radius 500 searches ~590 m around the centre and a user may see stations up to ~680 m away. `meta.cell` carries the hash; `distanceMeters` is measured from the cell centre.
  - Cache key: `(layers, cell, radiusBucket, providerVersions)`. New snapshot → new key → old entries just expire (LRU + TTL safety net). No explicit invalidation code.
  - Done in task 5.2 (`internal/api/cache.go`, `GetNearby`): key = cell, radius rounded up to 100 m, `layer=version` of each requested layer in registration order (the ids are in it). LRU of 1024 entries (~30 KB each as JSON), TTL 60 s as a safety net (the transit snapshot gets a new version at least every minute). Concurrent requests for one key wait for a single build, which runs with a context detached from the request (10 s limit), so one client going away does not fail the others. An answer is stored under the versions it was really built from, and not at all if a layer failed. The cached value has no `generatedAt`: every response gets the current server time, so the app's clock correction (ADR-0007) stays right. Metrics `livestop_cache_hits_total{result=hit|shared|miss}`, `livestop_cache_entries`.
  - `ETag` = hash of the key. `If-None-Match` match → `304` with empty body.
  - Response bodies gzip-compressed.
  - Done in task 5.3 (`internal/api/etag.go`, `gzip.go`): weak ETags (`W/"<128-bit sha256 hex>"`), because the bytes differ per response (`generatedAt`, gzip). `/v1/nearby`: ETag of the cache key; a matching `If-None-Match` (weak comparison, lists, `*`) answers 304 before any lookup or build; answers with a failed layer get no ETag. `/v1/stations/{id}`: ETag of `(id, snapshot version)`, checked after the station is built (it is small; saves the transfer). `withGzip` compresses 2xx JSON for clients that send `Accept-Encoding: gzip` (pooled writers; errors and 304s unchanged; `Vary: Accept-Encoding` always). Wedding 500 m: 31.9 KB → 2.9 KB. A 304 is possible while the snapshot version stays the same, i.e. between two realtime feeds (~30 s).
  - Measured in task 5.5 (README "Performance", tool `internal/tools/nearbyload`): 1000 simulated apps, one request each per 30 s — within 300 m (30 cells) cache hit ratio 93.5 %, within 3 km (787 cells) 13.0 %; a 200 takes p95 ≤ 6.9 ms either way (a build is one indexed PostGIS query plus snapshot lookups). 304s: 0.6 % with live realtime, because the ETag follows the snapshot version and every applied feed (~14 s) makes a new one; 50 % while the feed is paused. An ETag from a cell's content would match more often (PROGRESS open question #4).
- Middleware: request ID, structured access log, panic recovery, timeout, CORS (dev only).
- `/metrics` on a separate port (not exposed publicly).

### 3.7 Observability

- Logs: `log/slog` JSON, one line per request, plus component logs with `component=` attribute.
- Metrics (Prometheus): `livestop_gtfsrt_feed_age_seconds`, `livestop_gtfsrt_poll_total{result}`, `livestop_rt_match_ratio`, `livestop_snapshot_build_seconds{load}`, `livestop_snapshot_version`, `livestop_snapshot_departures`, `livestop_snapshot_refresh_failures_total`, `livestop_import_runs_total{trigger,result}`, `livestop_import_duration_seconds{result}`, `livestop_import_last_success_timestamp_seconds`, `livestop_http_request_duration_seconds{route,status}`, `livestop_cache_hits_total{result}`, `livestop_cache_entries`.
- `/v1/health`: `ok` | `degraded` (RT feed older than 2 min by `header.timestamp`, or schedule older than 3 days) | `down` (no schedule data).
  - Always HTTP 200 while the process runs; the body carries the status. The Docker healthcheck (`livestop healthcheck`) is therefore a pure liveness check and does not restart the container during e.g. a long first import.
  - Rules: `down` if the database is unreachable or there is no live schedule (`gtfs` schema); `degraded` if the transit layer is not `ok` (task 4.4: departures not refreshed for 3 min, no realtime data yet, or the merged feed older than 2 min by `header.timestamp`), or if the schedule was not confirmed current (import `succeeded` or `skipped`) in the last 72 h; the body carries `scheduleCheckedAt`.

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

`useNearby(client, params)` (`src/api/useNearby.ts`, task 3.2) is the TanStack Query hook for `/v1/nearby`:
- Query key: the request with defaults filled in (`radius` 500, `layers` sorted), so one request has one cache entry and one ETag. `params` null (no position yet) sends nothing.
- Each entry keeps `{ response, etag, checkedAt, clockOffsetMs }` (`Kept<T>`, `src/api/revalidate.ts`, shared with `useStation` for `/v1/stations/{id}` since 3.6). A refetch sends the entry's ETag as `If-None-Match`; a `304` returns the same `response` object with a new `checkedAt`, so nothing that reads the response re-renders.
- New parameters keep showing the previous area until their answer arrives (`keepPreviousData`, `isPlaceholderData`).
- No retries, `enabled: false`: the query never fetches on its own; the fetch policy decides (§4.2, wired in task 5.4).
- Arrays go out as `layers=a,b` (OpenAPI `style: form, explode: false`), set once in `createApiClient`.
- Path parameters are percent-encoded by openapi-fetch (`/v1/stations/de%3A11000%3A900009104`); the backend's `PathValue` decodes them (checked with curl, task 3.6).
- To check in 5.3 on devices: iOS (`NSURLSession`) and Android (OkHttp) may answer a conditional request from their own HTTP cache; with an explicit `If-None-Match` they are expected to pass the `304` through.

Location (task 3.3):
- `src/state/location.ts` (zustand): permission, kept device fix, mode (`device` or `mapCenter`), map centre (Berlin Mitte until the map sets it, 3.4). `useLocationView` gives the screen state: `checking`, `denied`, or `ready` with a position.
- `src/lib/location.ts` (expo-location, foreground only): on start and on every return to the foreground reads the permission and asks once if the OS has not asked yet; while granted and in the foreground shows the OS's cached position (≤ 5 min old) at once, then watches (`Balanced`, every ≥ 25 m / 5 s).
- Movement threshold (`acceptFix` in `src/core/location.ts`): a new fix replaces the kept one when it moved ≥ 25 m, is at least twice as precise, or is the first live fix; jitter and late older fixes are dropped. The 150 m refetch threshold (§4.2) is separate.
- Denied: a screen explains why, with "Allow location" (OS prompt again, or the settings app once the OS no longer asks) and "Use map center". While waiting for a first fix (e.g. location services off) the map centre is offered too. Granting later switches back to the device position.

### 4.2 Fetch policy (pure function in `src/core/fetchPolicy.ts`)

Fetch only when the app is in the foreground **and** at least one is true:
- moved > 150 m since last successful fetch,
- the earliest shown departure has passed,
- data age ≥ 30 s,
- user pulled to refresh.

Never more often than every 10 s. On error: backoff 5 s → 10 s → 20 s → 60 s (cap), counted from the failed request; the 10 s floor still applies, so the first retry comes after 10 s. Pull to refresh skips the backoff, not the floor; no other trigger skips it. On `304`: keep data, update "last checked" time.

`decideFetch(state, now)` returns fetch + reason (`initial`, `manual`, `retry`, `moved`, `departed`, `age`) or wait + reason + `until` (the time the answer can change without a new event, so the caller sets one timer instead of polling). "Earliest shown departure" is `earliestDepartureAt()` of the data right after the last successful request (task 3.1).

Wired in task 5.4 (`src/core/fetchLoop.ts`, `useNearby`):
- `createFetchLoop` runs `decideFetch` after every event — position (the request centre, which moves only after 150 m), foreground (AppState; anything but `background`), `refresh()`, a request that ended — and sets one timer for `until`; at most one request at a time. Clock, timers and the request are injected (pure TS, Vitest).
- Intervals use the **device clock**. The server-corrected clock (ADR-0007) changes with a response; measuring "10 s since the last request" across that change blocked requests for as long as the correction (found with a test server whose time was 4 days off). Only `earliestDepartureAt` is computed in server time (device now + the answer's offset) and handed back in device time.
- `useNearby` requests with `queryClient.fetchQuery` for the current key (same cache entry, 200 or 304). It reads nothing from the TanStack result during render: a read property becomes "tracked" and the hook then re-renders only when that property changes. The loop gets the latest key, clock and log through `useEffectEvent`; `refresh()` ("Try again") bumps a counter that an effect hands to the loop. The interim 30 s `refetchInterval` (3.5) is gone.
- Dev log (development builds, Expo terminal): `[nearby] request (<reason>), N in the last minute` per request, `[nearby] failed (n in a row): …` per failure. Standing still: ~2 per minute (`age`, plus `departed` when a departure leaves earlier); never more than 6 (10 s floor).

### 4.3 Offline / staleness (pure functions in `src/core/staleness.ts`)

- On app start, render the persisted response immediately (no spinner), then refetch.
  - Done in task 6.1 (`src/api/persistence.ts`, `src/lib/queryPersister.ts`, `app/_layout.tsx`): `PersistQueryClientProvider` with the AsyncStorage persister (key `livestop.query-cache`, written at most once a second, dropped after 24 h or when `PERSIST_BUSTER` changes). Only the **newest `/v1/nearby` answer** is kept (30–75 KB), not one per place visited and no station sheets. It is kept whatever its status and written as successful: a failed refetch while offline must not remove the last good data, and its error belongs to the old session.
  - Cold start (`app/index.tsx`, `LocationGate`): nothing is drawn while the cache is read (milliseconds). Then, while the permission or the first fix is pending, the map opens where the kept answer was fetched for (`lastNearbyCenter`, source `lastKnown`) instead of the "Finding your location…" screen. That place is the kept query's key, so the map has its data in the first render, and the fetch loop revalidates it at once with its ETag. When the device's position arrives the map follows it; more than 150 m away is a new request (the old area stays shown meanwhile). First start ever, or data older than 24 h: the location screen as before.
  - The kept answer includes the place it was requested for (the user's last request centre). It stays on the device.
- Departures in the past are dropped locally (`src/core/departures.ts`: one minute after their effective time, DATA_MODEL §9).
- Data age (since the last 200 or 304) < 60 s: normal. 60 s – 5 min: show "updated X ago". > 5 min: dimmed realtime values, show scheduled time emphasis. No network: banner "no connection — showing last known data".
  - Done in task 6.2. `statusBanner(checkedAt, now, failure)` in `src/core/staleness.ts` decides the text; `StatusBanner` (`src/ui/StatusBanner.tsx`) shows it at the top of the map:

    | Data age | Requests work | Last request failed |
    |---|---|---|
    | < 60 s | nothing | "No connection — showing last known data" / "updated just now" + "Try again" |
    | 60 s – 5 min | "Updated 3 min ago" | same title / "updated 3 min ago" |
    | > 5 min | "Updated 7 min ago" / "Live times may be out of date" (warning colours) | same title / "updated 7 min ago, live times may be out of date" |

  - **A failure is shown at once, whatever the age**: the next request is at most 30 s away, so the line appears within ~35 s of losing the connection and goes with the first answer (retries follow the backoff; "Try again" skips it). With data on the map a failure is this one line, not the "Could not load departures" box; the box is left for the case without any data.
  - **No network is inferred, not measured**: a request that ends without an HTTP status (no network, server down, 5 s timeout) is `unreachable` → "No connection"; an HTTP error is `server` → "Server problem" (`failureOf` in `src/api/client.ts`). No connectivity library: what matters is whether *our backend* answers, and a phone on Wi-Fi without internet would read as online.
  - **Ages use the device clock.** `checkedAt` is a device time, so the age is read with `useDeviceNow` (the clock store's time minus the server correction, ADR-0007). The banner re-renders when its text changes (once a minute).
  - **Stale (> 5 min):** the map screen computes one boolean and hands it to the layers (`stale` prop of `Marker` and `Sheet`, §4.4); it changes only when the 5 min line is crossed. In the transit layer live countdowns (on time, early, late) turn grey with their delay still shown, timetable times keep their colour — they are what still holds — and cancelled stays red and struck through. Screen readers hear "outdated live time". A marker redraws its native snapshot when `stale` flips. The station sheet uses the age of its own `/v1/stations/{id}` answer once it has one, and its legend line explains the grey.
- Device clock: if `meta.generatedAt` and the device time differ by more than 30 s when a response arrives, "now" is corrected by that offset (`clockOffset` in `src/core/time.ts`, ADR-0007).

### 4.4 Layer registry (mobile)

```ts
interface MapLayer<TFeature> {
  id: LayerId;                                  // "transit"
  select(response: NearbyResponse): TFeature[]; // pick this layer's features
  key(feature: TFeature): string;               // stable React key
  Marker: React.ComponentType<{ feature: TFeature; onPress: (f: TFeature) => void; stale: boolean }>;
  Sheet: React.ComponentType<{ feature: TFeature; client: ApiClient | null; stale: boolean }>; // sheet content; stale: data older than 5 min (§4.3)
  refresh: { minIntervalMs: number };
}
```

The map screen iterates over enabled layers; it does not know what a "departure" is.

Built in task 3.4 (`src/layers/registry.ts`, `src/layers/index.ts`) with `id`, `select`, `key` and `Marker`; `Sheet` and the marker's `onPress` came with the station sheet (3.6), `refresh` comes with the fetch policy (5.4). The map screen owns the bottom sheet (`@gorhom/bottom-sheet`, snap points 45 % / 90 %, swipe down closes it) and renders the tapped layer's `Sheet` inside; the root layout wraps the app in `GestureHandlerRootView`. `defineLayer<T>()` hides a layer's feature type in the list (`AnyMapLayer`): the screen only hands a layer its own features.

`now` is not a prop (task 3.5): a prop would re-render the map screen and every marker each second. Components read it from the clock store (`src/state/clock.ts`) with `useNow(select)`, which re-renders only when the selected value changes. One `setInterval` (1 s) ticks the store while the map is shown, plus a tick when the app returns to the foreground; "now" includes the server clock offset of the last `/v1/nearby` answer (ADR-0007).

Transit layer (task 3.5, `src/layers/transit`): one feature per **pole** (`{ station, pole }`), drawn by `PoleMarker`: up to 4 rows (line badge, headsign, countdown to the next running departure), then "+N more". Only directions with an upcoming departure get a row (`markerRows`); the others — night lines by day, rail replacement outside its hours, suspended lines — are left to the station sheet (3.6), and a pole with none draws no marker (user decision 2026-11-09: the map answers "what comes next near me"). Badge colour = API `color`, else a per-mode default (`MODE_COLORS` in `src/core/directions.ts`; tram/bus/ferry from VBB's generic rows); badge text white unless white has < 3:1 contrast. Its accessibility label reads station and rows.

Station sheet (task 3.6, `StationSheet`): the tapped pole's whole station from `/v1/stations/{id}` (`useStation`; until it loads, the `/v1/nearby` part). Per pole (titled by `poleTitle`: VBB's description without the bus/tram stop prefix, e.g. "Müllerstraße ht. Lindower Straße" or "S-Bahnsteig Gleis 1"; else "Platform N", else "Stop N"; DATA_MODEL §7 #28) the running directions with their next 3 departures (first one bold, cancelled ones struck through); then one "No departures soon" section with every direction that has nothing coming, once each, dimmed, with its platform (user decision 2026-11-09). Built by `sheetSignature`/`sheetModel` in `src/core/sheet.ts`: the clock selector returns one plain string per direction, so the sheet re-renders when a label changes, not every second.

Realtime display (task 4.5, `src/core/realtime.ts`, `DepartureTime`): marker rows and the sheet show each departure as a cell — countdown to the effective time (predicted ?? scheduled) plus a state: timetable (no realtime: the row's colour), live on time (green), live early (green, "−1"), live late (red, "+2"), cancelled (red, struck through). Delays are whole minutes toward zero, so under a minute counts as on time. Cells are encoded as plain strings (`encodeCell`) inside the clock selectors (`nextCells`, `sheetSignature`), keeping the once-a-minute re-render rule (§4.5). The sheet ends with a one-line colour legend.

Map screen (`src/ui/MapScreen.tsx`, task 3.4):
- Opens centred on the position (`initialRegion`, 500 m around it) and follows the device position until the user drags the map (`onPanDrag`; `isGesture` of `onRegionChangeComplete` is Google Maps only). In "map center" mode the camera never moves by itself.
- Reports its centre to the location store on every `onRegionChangeComplete`, so "map center" mode looks where the map is.
- Requests `/v1/nearby` for a request centre that moves only after > 150 m (`nextRequestCenter`), so small moves and pans reuse one query (and its ETag).
- Without a configured backend it shows `BackendStatus`. A failed request without any data shows the message with "Try again"; with data on the map it is a line in the `StatusBanner` at the top (§4.3, task 6.2).
- Data attribution (task 3.7, §6): `meta.attribution` of the shown answer, bottom right on the map (Apple Maps' "Legal" is bottom left) and at the end of the station sheet (`src/ui/Attribution.tsx`).
- Developer tool (development builds only, `__DEV__`): "Dev: place" (`src/ui/DevPlacePicker.tsx`) jumps to one of the spot-check stations (task 2.6) or back to "My location". It uses the location store's `lookAt(place)`: map-centre mode at that place plus a `jump` the map follows once. iOS has no way for an app to fake the GPS position, so this is how other parts of Berlin are tested on a phone.
- Expo Go has the native maps module (Apple Maps on iOS, Google Maps on Android). A development or store build for Android will need a Google Maps API key in `app.json` (not needed before Phase 7).

### 4.5 Map performance rules

- Re-render markers only when the **displayed minute** changes, not every second. (`PoleMarker` selects its labels from the clock store; tested with a React Profiler: no render for 59 ticks, one when a label changes.)
- `react-native-maps` custom markers: keep `tracksViewChanges` false except right after content changes (`PoleMarker`: on for 500 ms after its rows or labels change; only Google Maps uses it, Apple Maps ignores it).
- Cluster / hide poles at low zoom; show at most next departure per direction on the marker; the rest in the sheet.

### 4.6 Tooling and tests

- **Vitest** (plain Node) runs `src/core/**/*.test.ts` and Node-only tooling tests in `tests/node/` (e.g. the ESLint import-boundary test). `npm test` measures `src/core` coverage (`@vitest/coverage-v8`) and fails below 90 % lines, branches, functions or statements.
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

- VBB GTFS and GTFS-RT: CC BY 4.0 → show "Data: VBB (CC BY 4.0)" in the app (map overlay/about) and in API `meta.attribution`. The app shows the API's text wherever it shows the data: map bottom right, end of the station sheet (task 3.7).
- Map: Apple Maps (iOS) / Google Maps (Android) via `react-native-maps`. No cloud map IDs (keeps Google Maps native usage free).
