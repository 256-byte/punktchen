# Progress

> Updated by Claude Code at the end of every task. Newest session log entry on top.

## Current status

- **Active phase:** Phase 3 — Mobile MVP (Phase 2 done 2026-11-09)
- **Next task:** 3.6 Station sheet (`@gorhom/bottom-sheet`, all poles/directions/next departures)
- **Blocked by:** 0.9 only — GitHub Actions does not start jobs ("account is locked due to a billing issue"); user has a ticket open with GitHub

## Task status

| Phase | Task | Status | Notes |
|---|---|---|---|
| 0 | 0.1 Repo layout | done | Empty dirs kept via `.gitkeep`; remove each once the dir has real files. Added `.gitattributes` (LF) |
| 0 | 0.2 Docker Compose | done | |
| 0 | 0.3 Go skeleton | done | |
| 0 | 0.4 Migrations | done | |
| 0 | 0.5 OpenAPI v0 + health | done | layers/nearby/stations returned 501 until 2.5 (implemented there) |
| 0 | 0.6 sqlc | done | sqlc runs in Docker (pinned image); `go generate ./...` needs Docker |
| 0 | 0.7 Expo app | done | Expo Go needs a signed-in Expo account on PC and phone (see session log) |
| 0 | 0.8 Phone → backend networking | done | Restart Expo with `--clear` after installing packages |
| 0 | 0.9 CI | blocked | Workflow merged (PR #1); jobs not started: GitHub account billing lock. Re-run when unlocked |
| 0 | 0.10 Real commands in CLAUDE.md | done | All commands run from the repo root; tested in Windows PowerShell 5.1 |
| 1 | 1.1 Data exploration | done | DATA_MODEL §7 #1–#20; all (verify) items answered. Default GTFS URL and RT rate limit updated |
| 1 | 1.2 Downloader | done | Changed zip stays `running` until the importer (1.3) closes it |
| 1 | 1.3 Streaming import | done | Full VBB feed in ~9 s, peak Go memory 18 MB |
| 1 | 1.4 Indexes + in_scope | done | 10,841 poles / 3,914 stations in scope (2,974 in Berlin) |
| 1 | 1.5 Derived tables | done | `service_dates` now covers the whole feed (no daily rebuild) |
| 1 | 1.6 Sanity checks + swap | done | 0 failed reads in ~4,000 during 6 swaps; real feed passes default limits |
| 1 | 1.7 Scheduler | done | Dev DB now has the live schedule (startup import in Docker) |
| 1 | 1.8 Nearby query | done | Wedding 500 m: median 2.8 ms on the full feed |
| 2 | 2.1 Layer registry | done | Failing layer → `down`, others still served; race-clean (Docker) |
| 2 | 2.2 Schedule window loader | done | Range loader `[from, to)`; new derived table `trip_info` + index `stop_times(departure_secs)` |
| 2 | 2.3 Snapshot | done | Builder moves the window by the new minute only; race-clean (Docker). Found: no re-import after a layout change (fixed in PR #15) |
| 2 | 2.4 Transit provider | done | Golden JSON for Wedding; directions merged + trimmed (174 → 51 at Wedding); colours incl. VBB list |
| 2 | 2.5 API handlers | done | Contract tests validate every response against openapi.yaml (kin-openapi); ETag/cell stay for Phase 5 |
| 2 | 2.6 Spot check vs. VBB | done | 5 stations, 69 directions: all scheduled times match fahrinfo.vbb.de |
| 3 | 3.1 `src/core` | done | time, departures, staleness, geo, fetch policy; coverage 100 % (gate 90 % in `npm test`) |
| 3 | 3.2 API client + `useNearby` | done | 304 keeps the same response object, new `checkedAt`; backend sends ETags from 5.3 |
| 3 | 3.3 Location | done | Denied → explanation + "Allow location"/"Open settings" + "Use map center"; fixes kept from 25 m moves; iPhone: prompt + map OK (user, 2026-11-09) |
| 3 | 3.4 Map screen | done | Opens centred on the user, follows until a drag; layer registry with a plain station pin; request centre moves after > 150 m; iPhone: opens on the user, pins shown (user, 2026-11-09) |
| 3 | 3.5 Transit layer markers | done | One marker per pole, up to 4 rows; labels re-render once a minute (Profiler test); iPhone: countdowns and "now" update correctly (user, 2026-11-09) |
| 3 | 3.6 Station sheet | todo | |
| 3 | 3.7 Attribution | todo | |

Status values: `todo` · `in progress` · `done` · `blocked`. Add rows for the next phase when it becomes active.

## Decisions (short log, details in ADRs)

| Date | Decision | ADR |
|---|---|---|
| 2026-11-05 | Monorepo, Go backend, RN + Expo mobile, Postgres/PostGIS | 0001, 0009 |
| 2026-11-05 | Modular monolith, single binary | 0002 |
| 2026-11-05 | OpenAPI contract-first | 0003 |
| 2026-11-05 | Import via staging schema + atomic rename | 0004 |
| 2026-11-05 | Immutable snapshot behind atomic pointer | 0005 |
| 2026-11-05 | Geohash cell cache keyed by provider versions | 0006 |
| 2026-11-05 | Absolute timestamps, countdown on device | 0007 |
| 2026-11-05 | Generic Provider/Layer system, transit only | 0008 |
| 2026-11-08 | Default static feed `https://unternehmen.vbb.de/gtfs` (official link, current, matches GTFS-RT); change detection by SHA-256 | — (config, DATA_MODEL §7 #15) |
| 2026-11-08 | GTFS-RT limit 7 req/min: CLAUDE.md rule 7 updated, poll interval floor 10 s | — (config, DATA_MODEL §7 #20) |
| 2026-11-08 | User: keep config/docs in line with current upstream facts without asking first | — |
| 2026-11-09 | PLAN updated from 1.1 findings (user approved): 1.2 SHA-256 skip criterion, `stations` table (DATA_MODEL §3.4), loop/ring rules, real Wedding subset fixture, 32 h window, line colours, new 4.0 coverage check, realtime = has `delay` | — (no architecture change) |
| 2026-11-09 | Each import stamps a layout id (hash of the gtfs SQL + `layoutRevision`) on the schema; a missing or different stamp forces a full re-import even if the feed is unchanged | — (import detail, ARCHITECTURE §3.1) |
| 2026-11-09 | VBB line colour list committed and embedded (exception to CLAUDE.md rule 11, user-approved) | — (DATA_MODEL §7 #25) |
| 2026-11-09 | API directions per pole: terminal out, same label merged, per line and direction of travel only the ones with departures (else the most frequent, empty) — user decision | — (DATA_MODEL §3.2) |
| 2026-11-09 | Lines suspended for weeks (e.g. RE at Alexanderplatz until 2026-12-13) stay listed with an empty departure list; revisit after seeing it in the UI (3.5). A "runs within N days" filter would not hide them for good: it is evaluated against the calendar, so they come back on their own — user decision (former open question #4) | — (DATA_MODEL §3.2) |

## Open questions

| # | Question | Owner | Needed by |
|---|---|---|---|
| 1 | Server for deployment | user | later |
| 2 | Alert sources: format/terms of VBB Verkehrsmeldungen | Claude Code (research) | Phase 8 |
| 3 | Final app name | user | Phase 7 |

## Session log

<!-- Template:
### YYYY-MM-DD — task X.Y
- Done:
- Tests:
- New deps (+ why):
- Data facts found:
- Next:
-->

### 2026-11-09 — task 3.5
- Manual check of 3.3/3.4 by the user (iPhone, Expo Go): location prompt shown and granted, map (Apple Maps) opens at the user, station pins shown, no problems seen.
- Done: `src/core/directions.ts` (pure: `MODE_COLORS` defaults, `badgeColor`, `textColorOn` (white unless < 3:1), `directionKey`, `markerDirections` (4 rows + "+N"), `nextLabels`), `src/state/clock.ts` (clock store, `useNow(select)`, `useClockTicker`: 1 s interval + tick on foreground, server clock offset), `src/layers/transit/PoleMarker.tsx` (one marker per pole: badge, headsign, countdown, "—" when nothing is left; `tracksViewChanges` for 500 ms after a change; accessibility label), transit layer now selects poles (`PoleFeature`). Map screen runs the ticker and passes the offset. The 3.4 placeholder `StationMarker.tsx` is removed. Interim: `useNearby` refetches every 30 s until the fetch policy is wired (5.4), so countdowns keep their departures.
- Tests: Vitest `src/core/directions.test.ts` (13; core coverage 100 %). Jest `tests/ui/transit-markers.test.tsx` (7): rows/labels/"+1 more"/accessibility, countdown moves on to the next departure, **no re-render for 59 ticks, exactly one when a label changes** (React Profiler), `tracksViewChanges` on/off, one marker per pole, ticker with offset. Map screen tests use poles now. Typecheck, lint pass.
- Found: minutes are rounded down, so a departure at 14:33 shows "3 min" only at 14:30:00 sharp and "2 min" from 14:30:01; labels change just after the whole minute.
- Manual check (user, iPhone, Expo Go, an S1 station in south-west Berlin): all directions of the pole in one marker, one row each (Wannsee, Zehlendorf, Frohnau, Oranienburg), countdowns count down each minute, "now" shows correctly, no jank seen. Acceptance met.
- New deps: none.
- Next: 3.6 station sheet.

### 2026-11-09 — task 3.4
- Done: `src/layers/registry.ts` (`MapLayer` with id/select/key/Marker, `defineLayer`), `src/layers/transit` (selects stations, plain pin per station until 3.5), `src/layers/index.ts` (`mapLayers`), `src/ui/MapScreen.tsx` (react-native-maps: opens centred on the position, follows the device until a drag, reports its centre to the store, `/v1/nearby` around a request centre, backend-missing and error cards), home screen = location gate → map. Core: `regionAround` (geo), `nextRequestCenter` (fetch policy: moves after > 150 m). `useNearby` accepts a null client (backend not configured → no request).
- Tests: Vitest +4 (`regionAround`, `nextRequestCenter`; core coverage 100 %). Jest `tests/ui/map-screen.test.tsx` (8) with a react-native-maps stand-in (`tests/ui/maps-mock.tsx`): opens centred on the user (acceptance criterion), follows until a drag, map-centre mode keeps the camera and reports the centre, stations drawn through the registry, new request only after > 150 m, missing backend, error + try again, transit `select` with a down layer. Home screen test now opens the map. Typecheck, lint pass.
- Not checked yet: on a phone (Expo Go): map on start, follow, deny → "Use map center" → pan.
- New deps (+ why): `react-native-maps` 1.29.11 (the map; in the stack, CLAUDE.md §3; installed with `npx expo install`, part of Expo Go).
- Next: 3.5 transit layer markers.

### 2026-11-09 — task 3.3
- Done: `src/core/location.ts` (pure: `acceptFix` movement threshold 25 m / 2× precision, `locationView`, `BERLIN_CENTER`), `src/state/location.ts` (zustand store + `useLocationView`), `src/lib/location.ts` (`useDeviceLocation`: permission on start and on each return to the foreground, last known ≤ 5 min, watch while in the foreground; `requestLocationAccess`: prompt again or open the settings app), `src/ui/LocationGate.tsx` (checking / denied screen with "Use map center"), home screen shows where it looks until the map (3.4). `app.json`: expo-location plugin with the iOS "when in use" text (used by builds; Expo Go shows its own).
- Tests: Vitest `src/core/location.test.ts` (13, coverage stays 100 %); Jest `tests/ui/location.test.tsx` (9) with an in-memory expo-location (`tests/ui/location-mock.ts`): ask once, last known then live, jitter ignored and 40 m move kept, watch removed on unmount, denied screen + "Use map center" (acceptance criterion), "Allow location" after denial, "Open settings" when the OS no longer asks, back to the device after granting, map centre while waiting. Home screen test updated. Typecheck, lint pass.
- Not checked yet: on a phone (Expo Go). Worth a look in 3.4 when the map is there: deny → screen → "Use map center"; grant in Settings → back to the device.
- New deps (+ why): `expo-location` ~58.0.20 (device position and permission; in the stack, CLAUDE.md §3; installed with `npx expo install`, part of Expo Go), `zustand` ^5.1.0 (location store; in the stack).
- Next: 3.4 map screen.

### 2026-11-09 — task 3.2
- Done: `src/api/nearby.ts` `fetchNearby` (sends `If-None-Match` from the kept answer; 304 → same `response`, new `checkedAt`; 200 → new data, ETag, device clock offset from `meta.generatedAt`; 304 without a kept answer and error bodies → `ApiError`). `src/api/useNearby.ts` hook (normalized query key, `skipToken` without params, `keepPreviousData`, no retries, 30 s `staleTime`). `createApiClient` now serializes arrays as `layers=a,b` like the contract (openapi-fetch's default would send `layers=a&layers=b`).
- Tests: `tests/ui/use-nearby.test.tsx` (Jest, mocked fetch, 12 tests): URL and comma-separated layers, 200 keeps ETag/time/offset, 304 keeps the same response object and moves `checkedAt` (acceptance criterion), unexpected 304, error messages, query key defaults, hook refetch with 304 and 200, previous area shown while new params load (new key sends no ETag), no request without params, no retry. All mobile checks pass (Vitest 116, Jest 19, coverage gate).
- Found: openapi-fetch returns a 304 as an error with an empty body, so `fetchNearby` checks the status first. In hook tests, TanStack Query re-renders only for result fields read before (tracked properties): read `data` before a refetch.
- New deps: none.
- Next: 3.3 location.

### 2026-11-09 — task 3.1
- Done: pure functions in `mobile/src/core` (no React/RN/Expo imports):
  - `time.ts`: `parseTimestamp` (strict RFC 3339, offset required), `wallClock` ("HH:mm" as written, Berlin), `minutesLeft` (rounded down), `countdownLabel` ("now" / "N min" / "HH:mm"), `clockOffset` (device clock correction if > 30 s off, ADR-0007).
  - `departures.ts`: `effectiveDeparture` (predicted ?? scheduled + label), `hasDeparted` (one minute after the effective time), `upcomingDepartures` (drops departed/invalid, ordered by effective time, keeps cancelled), `nextDeparture` (skips cancelled), `earliestDepartureAt` (for the fetch policy).
  - `staleness.ts`: `freshness` (fresh < 60 s, aging ≤ 5 min, stale), `updatedAgoLabel`.
  - `geo.ts`: `distanceMeters` (haversine).
  - `fetchPolicy.ts`: `decideFetch(state, now)` → fetch + reason or wait + reason + `until`; ARCHITECTURE §4.2 made precise: the 10 s floor also holds after errors (first retry after 10 s, not 5 s); pull to refresh skips the backoff, not the floor. Wiring into `useNearby` stays for 5.4.
  - Former open question #4 decided by the user: suspended lines stay listed (see Decisions).
- Tests: 116 Vitest tests (102 new in `src/core`); `src/core` coverage 100 % statements/branches/functions/lines. `npm test` now runs Vitest with `--coverage` and fails below 90 % (thresholds in `vitest.config.mts`). Typecheck, lint, Jest pass.
- New deps (+ why): `@vitest/coverage-v8` ^5.1.0 (dev) — measures the coverage the 3.1 acceptance criterion asks for; the official provider for Vitest 5.1, same major version.
- Data facts found: none.
- Next: 3.2 API client + `useNearby` with ETag.

### 2026-11-09 — task 2.6
- Done: manual spot check of `/v1/stations/{id}` (dev DB, live feed sha256 `d9625924…`, files of 2026-11-05; snapshot 13:06) against VBB's official departure board (https://fahrinfo.vbb.de/webapp, tab "Abfahrten", Monday 2026-11-09 ~13:08–13:15). VBB shows the scheduled time with the delay separately (`13:10 +2 Minuten`), so scheduled times compare directly. Compared every departure both lists show (ours: up to 10 per direction within the 92-min window).

  | Station | Modes checked | Directions | Result |
  |---|---|---|---|
  | S+U Wedding (`de:11000:900009104`) | S15, S41/S42 Ring, U6, bus 120/147 | 10 | all times match |
  | S+U Alexanderplatz Bhf (`de:11000:900100003`) | S3, S5, S7, S9, U2, U5, U8; no regional today | 19 | all times match |
  | U Eberswalder Str. (`de:11000:900110006`) | U2, tram M10, replacement buses M1/12 | 8 | all times match |
  | S Wannsee Bhf (`de:11000:900053301`) | RE1, RE7, RB21, RB37, S1, S7, 8 bus lines, ferry F10 | 23 | all times match |
  | U Hermannplatz/Sonnenallee (`de:11000:900078105`) | bus M29, M41, 171, 194 | 9 | all times match |

  **69 directions, 0 scheduled-time differences.** Also matched: U6 north ends at U Kurt-Schumacher-Platz today (VBB too), S42 13:23/13:28 exist in the schedule (VBB: cancelled, realtime).
- Differences that are not times (DATA_MODEL §7 #27):
  - Lines suspended for weeks are listed with an empty departure list (the "most frequent direction" fallback of §3.2): RE2/RE7/RB14/RB23 at Alexanderplatz (feed: no stop there until 2026-12-13), trams M1/12 at Eberswalder Str. (replacement buses run). VBB does not list them. → open question #4.
  - Headsigns: RE1 "Hauptbahnhof" (VBB: "Magdeburg, Hauptbahnhof") and 114 "S Wannsee Bhf" (loop; VBB: "Heckeshorn") come from the GTFS `trip_headsign` itself; 147 "S Ostbahnhof" (VBB adds "via S+U Hauptbahnhof").
  - VBB's board for a station also lists nearby stops (Alexanderplatz/Gontardstr. trams, S Wedding/Martha-Ndumbe-Platz bus M27/247, U Hermannplatz U7/U8); we keep them as separate stations (DHID, §7 #4, #23). Long-distance trains (e.g. 1321 at Wannsee) are not in the VBB GTFS.
- Tests: manual (this check); no code changed.
- New deps (+ why): none.
- Next: Phase 2 done. Phase 3 (mobile MVP) starts with 3.1 `src/core`.

### 2026-11-09 — task 2.5
- Done:
  - `internal/api/layers.go`: `ListLayers` (registry), `GetNearby` (validation → `layer.Registry.Nearby` → typed `layers.transit` + `meta.layers` per layer), `GetStation` (transit provider). Interfaces `Layers` and `TransitStations` in `api.Config`; `cmd/server` builds the registry with the transit provider. 501 stubs removed.
  - Validation: lat/lng ranges, radius 100–1000 (default 500), layers (default `transit`; unknown, empty, repeated → 400); all problems in one 400 message. Errors: 404 `not_found` (unknown station), 503 `unavailable` (sheet before the first snapshot), 500 `internal` (logged). A failing layer → 200 with `down` in meta and no payload (registry semantics).
  - `Cache-Control: no-cache` on 200s. `cell`, `ETag`/304 and gzip stay for Phase 5 (5.1–5.3).
  - Contract: descriptions only (nearby: a down layer is missing from `layers`; station: 404/503). Go + TS regenerated.
  - Docs: ARCHITECTURE §3.6, DATA_MODEL §5.3 (status/code table); CLAUDE.md note on the transient testcontainers error.
- Tests: unit (fake layer through the real registry) — `/v1/layers`; nearby defaults (radius 500, transit), radius/layers passed on, 8 bad requests with their messages, meta (`generatedAt` Berlin, attribution, version, `realtimeAgeSeconds`, no `cell`), `Cache-Control`; down layer → 200, `down`, no payload, no attribution; station 200/404/503/500. **PLAN criterion:** `TestContract` — 12 requests on the Wedding fixture (17:00) and on an empty state (health, layers, nearby default/1000 m/empty area/no departures, station sheet, unknown station, bad radius, unknown layer, missing lat, station without departures); every response validated against `api/openapi.yaml` (kin-openapi `openapi3filter.ValidateResponse`, JSON Schema 2020-12); `openapi.yaml` itself passes `doc.Validate`; a broken body is rejected (the validator really runs). vet, golangci-lint 0 issues, mobile typecheck + lint, all backend tests green (`-p 2`).
- Manual check (Docker Compose, dev DB, full feed, 12:47): `/v1/layers` 96 B; `/v1/nearby` Wedding 26 KB in 60 ms (first request), Alexanderplatz 1000 m 133 KB in 34 ms, `/v1/stations/de:11000:900009104` 16 KB in 14 ms; radius=5 → 400, `/v1/stations/nope` → 404 with Error JSON. Departure times plausible (U6 every 5 min, S41/S42 Ring, 120 every 20 min).
- Observed: twice "rootless Docker is not supported on Windows" from testcontainers when the whole backend suite ran next to other Docker work; a rerun (or `-p 2`) passed. Noted in CLAUDE.md §5.
- New deps (+ why): `github.com/getkin/kin-openapi` moved from indirect (via oapi-codegen) to direct, **tests only**: validates responses against the OpenAPI 3.1 contract. No new module downloaded except the `gorilla/mux` checksum in go.sum (kin-openapi's module graph); nothing new in the binary.
- Next: 2.6 spot check vs. official VBB departures for 5 stops (manual).

### 2026-11-09 — task 2.4
- Done:
  - `internal/transit`: `Provider` implements `layer.Provider` (`Info`, `Nearby`, `Version`, `Health`) plus `Station(id)` for the sheet. Payload = generated `*openapi.TransitLayer`. Per request two queries (`NearbyPoles`, new `PoleDirections`; `StationPoles` for the sheet) and departures from the snapshot (3 per direction, 10 on the sheet), starting at the snapshot's `GeneratedAt` so the response depends only on its version.
  - Direction rules per pole (user decision, DATA_MODEL §3.2): terminal rows without departures out; same label (mode, line, display headsign, ring) merged; per line and direction of travel only directions with departures, else the most frequent one with an empty list. Wedding 500 m: 174 rows → 51 directions.
  - Colours: `route_color`, else VBB's official list, else null. The list (`backend/internal/transit/data/linienfarben.csv`, 14 KB, CC BY 4.0) is committed and embedded, matched by (agency, line, mode) — user-approved exception to CLAUDE.md rule 11, README with source and update steps.
  - Mode order (regional, sbahn, ubahn, tram, bus, ferry, other), natural line order, " (Berlin)" dropped from names and headsigns. Mode mapping stays in derived.sql (`modeOf` in Go only for the colour list).
  - `departures.BuilderConfig.Now` (clock for tests). Contract: descriptions of `Station.modes`, `Pole.directions`, `Direction.color` (no shape change), Go + TS regenerated.
  - Docs: ARCHITECTURE §3.5, DATA_MODEL §3.2, §5.2, §7 #25–#26; CLAUDE.md rule 11.
- Tests: unit — colour list (spot checks, R/G/B = hex column on every row that has both, parse errors), `lineColor` precedence (route_color, list, rail replacement bus gets none), `modeOf` = DATA_MODEL §6, natural line order, `displayName`. Integration `TestProviderRules` (synthetic feed): station/mode/direction order, terminal rule, merge of two trip headsigns, empty short-turn hidden, night line shown once with no departures, max 3, departures from the snapshot time, platform, station sheet (10, no distance, unknown id), empty result `{"features":[],"layer":"transit"}`; no snapshot → `ErrNoDepartures` / `down`; health `degraded` after 3 min. **PLAN criterion:** `TestProviderWeddingGolden` — `/nearby` around U Wedding (500 m) and the S+U Wedding sheet at Tue 2026-11-10 17:00 equal `internal/transit/testdata/*.golden.json` (`-update` rewrites them; reviewed by hand; sheet poles: S-Bahn platforms, U-Bahn platforms, then bus poles). vet, golangci-lint 0 issues, mobile typecheck + lint, all backend tests green.
- Full feed (`TestImportRealFeed`, 2026-11-08 feed, test container right after the import, snapshot at 17:00): transit `Nearby` Wedding 500 m — 6 stations, 52 directions, 26 KB JSON, median **4.5 ms**; Alexanderplatz 1000 m (densest, max radius) — 27 stations, 297 directions, 135 KB JSON (uncompressed), median **12.1 ms**.
- Data facts found: §7 #25 (colour list vs feed), #26 (direction variants).
- New deps (+ why): none (`oapi-codegen/runtime/types` was already a dependency of the generated code).
- Next: 2.5 API handlers (`/v1/nearby`, `/v1/stations/{id}`, `/v1/layers`), wiring the registry and provider in `cmd/server`.

### 2026-11-09 — task 2.3
- Done:
  - `departures.Snapshot`: immutable, `Version`, `GeneratedAt`, `ScheduleID`, `From`/`To`; departures in one array grouped by pole (counting sort, stable), `Departures(stopID)` returns a capped slice so a reader's `append` cannot reach another pole; `Len()`, `Stops()`.
  - `departures.Builder`: window `[now − 2 min, now + 90 min)`, refreshed every minute. It keeps the departures it has and loads only the new part; full load at startup, on `Reload` (the importer calls it after a changed import), when the new part comes from another `ScheduleID`, and when the clock jumped. Publishes with `atomic.Pointer`; `Current()` never blocks. A failed refresh keeps the last snapshot; the next one continues where the last good window ended.
  - Version = Unix ms at process start, +1 per snapshot: monotonic across restarts too, so an ETag from before a restart cannot match new data (ARCHITECTURE §3.4).
  - Metrics `livestop_snapshot_build_seconds{load}`, `livestop_snapshot_version`, `livestop_snapshot_departures`, `livestop_snapshot_refresh_failures_total`.
  - `cmd/server`: builder runs next to the import scheduler (`runBackground` helper stops both before the pool closes).
  - Removed `Window.ByStop()` (replaced by the snapshot). Docs: ARCHITECTURE §3.2, §3.4, §3.7; DATA_MODEL §8 now shows the real types.
- Tests: unit with a fake loader — first refresh loads the whole window; same second → no new snapshot; moving window loads only `[old.To, new.To)` and equals a full load (steps 1 s … 89 min); new `ScheduleID` → whole window; `Reload` and clock jumps (+3 h, −2 h, −1 s) → whole window; failures keep the snapshot and recovery continues from the last good window; `ErrNoSchedule`; versions above the start time and above the previous process after a restart; old snapshots unchanged after 20 refreshes and a schedule change, `append` on a pole slice harmless; `Run` (first refresh, `Reload`, stop). **PLAN criterion:** `TestBuilderConcurrentReads` (8 readers, 300 refreshes incl. schedule changes) — `go test -race -count=3` clean in `golang:1.27.3` (Docker). Integration `TestBuilderWedding`: window moved 76 times from 23:30 across midnight equals a full load at every step (2,121 → 1,361 departures); a new import moves the snapshot to the new schedule. vet, golangci-lint 0 issues, all backend tests green.
- Found: **re-import missing after a layout change.** The dev DB's live schedule was imported before 2.2, so it had no `trip_info`; the feed was unchanged (same SHA-256), so the importer skipped it and the builder failed every minute (`relation "gtfs.trip_info" does not exist`). Fixed separately (branch `fix/reimport-on-layout-change`, PR #15: layout stamp on the schema forces a re-import).
- Manual check (Docker Compose, dev DB after the re-import, 2026-11-09 11:33 Berlin): first snapshot at startup with 81,705 departures at 8,470 poles in 1.68 s (cold container); one minute later an incremental refresh in 0.18 s (81,745 departures), version +1 (1794220416760 → …761 = start time in Unix ms), 0 failures. Metrics visible on `:9090/metrics`.
- New deps (+ why): none.
- Next: 2.4 transit provider.

### 2026-11-09 — fix: re-import after a layout change
- Found during 2.3: the dev DB's live schedule was imported before 2.2 (no `trip_info`). The feed was unchanged (same SHA-256), so every import was `skipped` and the departures loader failed with `relation "gtfs.trip_info" does not exist`. The same would happen after every change to `db/gtfs/*.sql`, and after a lost live schema while `meta.feed_imports` still lists the feed.
- Done:
  - `static.Layout`: hash of the gtfs SQL files + `layoutRevision` (Go constant, increase it when Go code changes what an import writes). The loader stamps it as the staging schema's comment; the swap carries it to `gtfs`.
  - Live schema missing or another stamp → `Status.Current` is false (the scheduler imports at startup) and `Importer.Run` calls `Downloader.Fetch(ctx, force=true)` (no conditional headers, no SHA-256 skip). No migration.
  - Docs: ARCHITECTURE §3.1, DATA_MODEL §3.3 (import log lifecycle).
- Tests: `TestLayoutOf` (revision, content and file boundaries change the id); downloader: forced fetch sends no validators and returns the unchanged file as changed; importer: an older stamp and a missing stamp are imported again with the same feed (new schema, `Current` true), then the next run is `skipped` again; a dropped live schema is imported again although the log has the feed. vet, golangci-lint 0 issues, all backend tests green.
- Manual check (Docker Compose, dev DB): startup logged "live schedule missing or from another layout", downloaded the unchanged feed (sha256 `d9625924…`), import succeeded in 4 min 13 s; `gtfs` now has the stamp and `trip_info` (263,715 rows). A restart afterwards did not import again.
- New deps (+ why): none.
- Next: 2.3 snapshot (branch `feature/departure-snapshot`), then check the builder on the dev DB.

### 2026-11-09 — task 2.2
- Done:
  - `transit/departures`: `ServiceDay` (start = noon − 12 h Europe/Berlin, `At(secs)`), `dayRanges` (half-open window → one seconds range per service day; looks back as far as the feed's `max(departure_secs)` needs), `Loader.Load(ctx, from, to)` → `Window{From, To, ScheduleID, Departures}` sorted by time, `ByStop()`. Repeated strings are interned.
  - A departure = boardable stop time: in-scope pole, `pickup_type` ≠ 1, not the trip's last stop, service runs that day (DATA_MODEL §3.6).
  - sqlc queries `ScheduledDepartures` (one service day, seconds range), `MaxDepartureSecs`, `LiveScheduleID` (OID of the `gtfs` schema). Load runs in a read-only tx with `plan_cache_mode = force_custom_plan` (the generic plan was 5–10× slower) and retries if an import swaps the schedule in between (OID before ≠ after).
  - Import: `trip_info` is now a kept derived table (DATA_MODEL §3.5, was a temp table) with `service_id`; new index `stop_times(departure_secs)`. Measured alternatives first: `unnest` over days, EXISTS for the last stop, an `UPDATE trips` (11.8 s) and a materialized CTE all gave worse plans or imports.
  - `static/statictest`: test helper for other packages (fresh DB, load a zip or files, make it live); used by 2.3/2.4 later.
  - Docs: ARCHITECTURE §3.2, DATA_MODEL §2, §3.5, §3.6, §7 #24, §8.
- Tests: unit — service day start/At incl. both 2026 DST days, 24:30 → D+1 00:30, 31:17 → D+1 07:17, night trips across the switch (second 02:30 in autumn), `dayRanges` (midnight, fractional seconds, feeds past 48:00). Integration (`TestLoadWindow`, synthetic feed) — the PLAN cases with absolute times, filters (no pickup, out of scope, last stop), headsign fallback and `NoDirection`, adjacent windows add up exactly, `ErrNoSchedule`, new schedule id after an import. `TestLoadWindowWedding`: loader equals a plain-SQL definition on real data (5,313 and 1,629 departures). `TestImportRealFeed` passes with the new table/index (timings DATA_MODEL §7 #24). vet, golangci-lint 0 issues, static package tests green.
- Data facts found: VBB night trips after midnight all belong to the previous service day (§7 #24).
- New deps (+ why): none.
- Next: 2.3 snapshot (load the window once, then extend it by 1-minute slices; full reload when `ScheduleID` changes).

### 2026-11-09 — task 2.1
- Done:
  - `internal/layer`: `Provider` (`Info`, `Nearby`, `Version`, `Health`), `NearbyQuery` with `Validate` (coordinate ranges, positive radius; the 100–1000 m product limit stays in the API), `Result{Version, Payload}`, `Health{Status, RealtimeAge, LastError}`, `Status` values matching the contract.
  - `Registry`: rejects empty/duplicate ids; `Layers()` and `Resolve(ids)` in registration order, duplicates removed, all unknown ids named in one `ErrUnknownLayer`.
  - `Registry.Nearby`: asks the requested layers concurrently and composes a `Composite` (per-layer version, health, payload, error + deduplicated attribution of the layers with data). A provider error or panic marks only that layer `down`; the request fails only for an invalid query, unknown layers or a cancelled context. The version is taken from the result, not read separately.
  - The package has no OpenAPI import; payloads are opaque and mapped to typed fields by the API (2.5). ARCHITECTURE §3.5 updated to the real interface (`Info()` instead of `Layer()`, so `/v1/layers` gets name + attribution from the provider).
  - Removed `backend/internal/layer/.gitkeep`. CLAUDE.md §5: how to run `-race` locally in Docker.
- Tests: fake-provider unit tests — registration errors and order, `Resolve` dedupe/order/unknown ids, query validation (ranges, NaN, Inf, radius ≤ 0), two-layer composition (query passed through, unrequested layer not asked, result version wins, health copied, attribution deduplicated), failing and panicking layers → `down` while the healthy one is served, layers run concurrently (barrier test), invalid query / unknown layer / cancelled context. `go test -race -count=3` clean in `golang:1.27.3` (Docker). vet, golangci-lint 0 issues.
- New deps (+ why): none.
- Next: 2.2 schedule window loader.

### 2026-11-09 — task 1.8
- Done:
  - sqlc query `NearbyPoles(lat, lng, radius_m)` (`db/queries/nearby.sql`): served (`pole_directions` exists), in-scope poles within the radius (`ST_DWithin` on `stops.geom`), joined with `stations`; rows grouped by station, ordered by station distance (= its nearest pole) then pole distance. Returns station id/name/location/modes and pole id/platform/location/distance. All PostGIS results cast to `float8`, so the Go types are exact.
  - `sqlc.yaml`: `strict_function_checks` turned off — sqlc's catalog does not know PostGIS functions (`function "st_y" does not exist`). Documented in the file.
  - DATA_MODEL §7 #23.
- Tests: `TestNearbyPolesWedding` on the Wedding fixture (loaded + swapped): S+U Wedding is one station with exactly its 6 served poles incl. bus `::3`/`::4`, nearest first; Martha-Ndumbe-Platz separate; stations ordered and not split; every pole inside the radius; empty area → no rows. Speed (acceptance < 20 ms): full feed in `TestImportRealFeed`, 25 runs on one connection: Wedding 500 m median 2.8 ms, Alexanderplatz 1000 m 11.9 ms; `EXPLAIN ANALYZE` on the dev DB confirms index scans only (first call ~90 ms with planning). vet, golangci-lint 0 issues, `go generate` up to date.
- Data facts found: same-name neighbouring stations with different DHIDs ("U Reinickendorfer Str."), §7 #23.
- New deps (+ why): none.
- **Phase 1 is complete.** Next: Phase 2 (2.1 layer registry) when the user starts it.

### 2026-11-09 — task 1.7
- Done:
  - `static.Scheduler`: daily at `LIVESTOP_GTFS_IMPORT_TIME` (Berlin wall clock, DST-safe), at startup when the live schema is missing, 1 h after a failure (never past the next daily run), and on demand (`Trigger`, one queued request; `ErrImportRunning` while running). "Another process holds the lock" counts as `busy`, not as a failure. Clock and timers are injectable.
  - `static.Metrics`: `livestop_import_runs_total{trigger,result}`, `livestop_import_duration_seconds{result}`, `livestop_import_last_success_timestamp_seconds` (renamed from ARCHITECTURE's `…_timestamp` to follow the Prometheus unit suffix rule).
  - `static.Status`: live schema present + last `succeeded`/`skipped` time (new sqlc query `LastScheduleCheck`).
  - Contract: `POST /internal/import` (tag `internal`, 202 `ImportStarted` / 404 unless `LIVESTOP_DEV=true` / 409 `conflict`), optional `Health.scheduleCheckedAt`, error code `conflict`. Go + TS regenerated.
  - `/v1/health`: `down` without DB or without live schedule; `degraded` if the schedule was not confirmed in 72 h.
  - `cmd/server`: wires downloader (User-Agent `livestop/<version> (+https://github.com/256-byte/punktchen)`), loader, importer, scheduler; the scheduler stops with the process and is awaited before the pool closes. `api.NewServer` now takes an `api.Config`.
  - JSON logs write durations as text (`"2m38s"`), not nanoseconds.
  - Docs: ARCHITECTURE §3.1 schedule, §3.7 metrics + health rules; DATA_MODEL §5.1; CLAUDE.md dev command.
- Tests: scheduler unit tests with a fake clock (next daily slot incl. both DST days and exact-time edge; startup import only without data; scheduled run; retry after failure and never past the daily slot; busy is not a failure; manual trigger and conflict while running; stop on cancel); API tests (health down/degraded/ok, `scheduleCheckedAt` in Berlin time, trigger 404/202/409); `Status` before/after an import; logger durations. vet, golangci-lint 0 issues, mobile typecheck/lint/test green.
- Manual check (acceptance) in Docker with `LIVESTOP_DEV=true` (root `.env`, git-ignored): empty dev DB → startup import ran automatically (158 s, 3,914 stations), health `down` during it and `ok` with `scheduleCheckedAt` after; `POST /internal/import` during the run → 409, afterwards → 202 and a `skipped` run (same sha256, 6 s); the daily 03:30 run happened to fire 13 s after the startup import → `skipped`; restart with data → no import, next run tomorrow 03:30. Metrics on :9090 show all three `livestop_import_*` series. Container memory peak 13.7 MiB during the import. `docker compose stop backend` during a manual import → clean stop in 12 ms, attempt closed as `failed` ("context canceled"), no row left `running`.
- New deps (+ why): `github.com/kylelemons/godebug` (indirect, pulled in by `prometheus/testutil` for metric assertions in tests).
- Next: 1.8 nearby-poles query.

### 2026-11-09 — task 1.6
- Done:
  - `static.check` (sanity.go): size floors (VBB defaults: 5,000 in-scope poles, 1 M stop_times) and max 50 % drop vs. the last successful import's `row_counts`; dangling trip/stop/route references; trips without running service or stop times; served poles without coordinates; stop times without departure; service in `[today, today + 7]` (Europe/Berlin). All checks run; all problems are reported in one error.
  - `swap` (swap.go): one transaction (`lock_timeout` 10 s) renames `gtfs` → `gtfs_old` and `gtfs_staging` → `gtfs`; `gtfs_old` is dropped afterwards in its own transaction (`lock_timeout` 60 s, failure only logged; the next swap drops leftovers).
  - `static.Importer.Run` (importer.go): advisory lock (`ErrImportRunning` for a concurrent run) → `Downloader.Fetch` → skip if unchanged → `Loader.Load` → `check` → `swap` → `ImportLog.Succeed` with row counts + `in_scope_poles`/`in_scope_stations`; any error → `Fail`, live schedule untouched, zip removed.
  - ARCHITECTURE §3.1 steps 5–6 + pipeline; DATA_MODEL §3.3 `row_counts` keys; CLAUDE.md real-feed test command now runs both real-feed tests.
- Tests: `TestSwapWithoutFailedReads` (acceptance): 4 readers on their own pool (prepared statements) query `gtfs.stops` and a `pole_directions` ⨝ `stations` join while 6 full imports swap the schema: 4,109–4,538 reads, **0 failed**, both old and new data seen, only `gtfs` left. `TestImporter`: first import creates `gtfs`; unchanged feed skipped (same schema oid); changed feed replaces data; failures (sanity, not a zip, expired feed, > 50 % drop) keep the old data and record `failed` with the reason; held lock → `ErrImportRunning`. `TestSanityChecks`: every check fires on a crafted feed, clean feed passes. Real feed (`TestImportRealFeed`, default limits): all checks pass (~8 s), swap 18 ms, whole run 2 min 14 s, second run skipped. Test helper `writeZip` now writes files in sorted order (map order made identical feeds hash differently). vet, golangci-lint 0 issues.
- New deps (+ why): none.
- Next: 1.7 scheduler (daily run + import on empty DB + dev endpoint), which calls `Importer.Run`.

### 2026-11-09 — task 1.5
- Done:
  - `db/gtfs/derived.sql` (embedded as `db.GTFSDerived`), run after keys/indexes and `ANALYZE` in the import transaction: `stops.station_id` (parent / DHID prefix / pole), `service_dates`, temp `trip_info` (ends, loop flag, instances), `pole_directions` (mode mapping, terminal/origin by the 95 % rule on non-loop trips, `is_ring`, `display_headsign`, `trip_count`), `stations` (name + location from the `location_type=1` row or the poles, sorted modes, in_scope, pole_count), with their keys/indexes. Tables declared in `schema.sql`, so sqlc knows them.
  - In-scope station count now comes from `stations` (the Go copy of the grouping rule is gone; the rule lives once, in SQL).
  - `gtfsexplore subset`: real-data fixture `testdata/gtfs/vbb-wedding-800m.zip` (2.7 MB) with README.
  - DATA_MODEL §3.1/§3.2/§3.4 updated, §7 #8 (32 trips without headsign), #21 (stop_headsign), #22 (numbers).
- Decisions (no ADR, data model only): `service_dates` covers the feed's whole validity instead of `[today−1, today+7]` rebuilt daily (~257 k rows; deterministic imports; 1.7 no longer needs a daily rebuild). `trip_count` = trip instances over the validity. `display_headsign` added because 72 % of stop headsigns differ from the trip headsign. `stops.station_id` column instead of a mapping table.
- Found: 32 trips without `trip_headsign` broke the NOT NULL `headsign` on the real feed → last stop's name, as the GTFS reference suggests.
- Tests: hand-written rules feed (service dates incl. added/removed dates; terminal/origin; 95 % rule 19/20 vs 9/10; loop bus and ring S41 not terminal; every mode mapping; display headsign 2 of 3; empty headsign; trip after midnight; station name/centroid/modes/unserved). Wedding subset: bus poles `::3`–`::8` → `de:11000:900009104`, modes bus/sbahn/ubahn, Martha-Ndumbe-Platz separate, U6 through, S41/S42 ring, **all GTFS-RT fixture trips present**. Real feed: see §7 #22. vet, golangci-lint 0 issues.
- New deps (+ why): none.
- Performance note: derived tables take ~1 min on the dev PC (the `pole_directions` aggregation over 5.8 M stop times); fine for a daily import, revisit if imports become frequent.
- Next: 1.6 sanity checks + atomic schema swap.

### 2026-11-09 — task 1.4
- Done:
  - `stops.in_scope boolean NOT NULL DEFAULT false` in `db/gtfs/schema.sql`.
  - `db/gtfs/indexes.sql` (embedded as `db.GTFSIndexes`): primary keys on routes, trips, stops, calendar, calendar_dates, stop_times; indexes `stop_times(stop_id, departure_secs)`, `trips(service_id)`, `trips(route_id)`, `stops(parent_station)`, GiST `stops(geom)`. No foreign keys (1.6 reports dangling refs instead).
  - `Loader.Load` now runs, in the same transaction after COPY: scope flags from `LIVESTOP_SCOPE_BBOX` (poles in the bbox; `location_type=1` stations with a pole in scope), counts of in-scope poles and stations, keys + indexes with `maintenance_work_mem = 256MB` (tx-local), `ANALYZE` of every table. Returns `LoadResult{Rows, InScopePoles, InScopeStations}`.
  - `stationKeySQL`: the station grouping of DATA_MODEL §3.4 as one SQL expression, to be reused by the `stations` table in 1.5.
- Tests: mini feed extended (Potsdam station + pole outside the bbox, entrance, non-DHID pole): flags per stop, 3 poles / 2 stations (parent and DHID-prefix poles grouped), all indexes present, `pg_stats` filled, duplicate `(trip_id, stop_sequence)` fails with `stop_times_pkey`; tables.go ↔ schema.sql check now skips DEFAULT columns. Real feed: 10,841 poles / 3,914 stations in scope, Berlin 2,974 stations; load 9 s + index/ANALYZE 23 s = 33 s, peak Go memory 22 MB. vet, golangci-lint 0 issues.
- Data facts found: DATA_MODEL §7 #12 (scope numbers, per AGS and mode; 1,184 in-scope poles without any stop_times).
- New deps (+ why): none.
- Next: 1.5 derived tables (`stations`, `service_dates`, `pole_directions`) + Wedding subset fixture.

### 2026-11-09 — task 1.3
- Done:
  - `backend/db/gtfs/schema.sql`: tables of schema `gtfs` (agency, routes, trips, stops, calendar, calendar_dates, stop_times) with all GTFS-reference columns, `*_secs` columns next to the raw times, `stops.geom` as a generated `geography(Point, 4326)` column. Embedded as `db.GTFSSchema`; also in `sqlc.yaml` so later queries are type-checked against `gtfs.*`. No keys/indexes yet (1.4, after loading).
  - `internal/transit/static.Loader.Load(zip)`: one transaction: drop + recreate `gtfs_staging` (DDL with `gtfs.` → `gtfs_staging.`), then each file straight from the zip through `encoding/csv` into `pgx.CopyFrom`. Per-column conversion (text/int/float/0-1 bool/YYYYMMDD date/GTFS time → raw + seconds), empty → NULL, required columns checked, errors name file + line. Row counts returned and logged per table.
  - Found while testing: a UTF-8 BOM in front of a quoted header (`"stop_id"`) breaks `encoding/csv` ("bare \" in non-quoted-field"); the BOM is now stripped from the byte stream before parsing.
- Tests: unit (time parsing incl. `9:20:00` and `31:17:00`, value conversion, tables.go ↔ schema.sql column check, staging DDL rewrite). Integration on a hand-made zip: values, BOM, after-midnight times, NULLs, missing optional column, reload replaces staging, bad value → error with line and previous staging kept, missing column/file, non-zip. Real feed (`LIVESTOP_GTFS_ZIP`): current VBB zip 5,836,294 stop_times in 8.1 s, whole load 9 s, **peak Go memory 18 MB** (acceptance < 500 MB); the older mastscharf zip: 6.2 M rows, 22 MB. Command lines in CLAUDE.md checked in PowerShell 5.1. vet, golangci-lint 0 issues, `go generate` up to date.
- New deps (+ why): none.
- Next: 1.4 indexes + `in_scope`.

### 2026-11-09 — task 1.2
- Done:
  - `internal/transit/static`: `Downloader.Fetch` opens a `running` row in `meta.feed_imports`, sends `If-None-Match` / `If-Modified-Since` only if the last **succeeded** import stored them, streams the body to a temp file while hashing (SHA-256, size cap 1 GiB), records validators + hash, and closes the row as `skipped` on 304 or same hash. A changed zip is returned as `Download{Path, SHA256, …}`; the importer closes the row with `ImportLog.Succeed/Fail` and calls `Remove`. Errors close the row as `failed`; closing works even with a cancelled context.
  - sqlc queries `StartFeedImport`, `RecordFeedDownload`, `FinishFeedImport` (`:execrows`, only updates `running` rows → no double close).
  - Zip goes to the OS temp dir (distroless has `/tmp`); no new config or volume needed.
  - DATA_MODEL §3.3: attempt lifecycle. CLAUDE.md §5: live test command.
- Tests: integration (fake HTTP server + PostGIS): no validators → skipped by sha256 and no file left; ETag → 304; Last-Modified → 304; server ignores validators → still skipped by hash; failed import is not a reference; HTTP 500 → `failed` row; size cap; cancelled download → row `failed`, no file; double close rejected. Live test (`LIVESTOP_LIVE_TESTS=1`, real VBB): 84.5 MB in ~5 s, second run `skipped` by sha256 in ~4.9 s. Command line checked in PowerShell 5.1. vet, golangci-lint 0 issues, `go generate` up to date.
- New deps (+ why): none.
- Next: 1.3 streaming import into `gtfs_staging`.

### 2026-11-08 — task 1.1
- Done:
  - `backend/internal/tools/gtfsexplore` (dev tool, not in the server binary): `download`, `static` (report), `stops` (list by id/name), `record` (GTFS-RT samples + one conditional GET), `rt` (RT vs. static report), `trim` (fixtures around a point).
  - Findings in DATA_MODEL §7 #1–#20; all **(verify)** marks removed (§1 Station, §3.2 `station_id`/`direction_id`, §5.2 ids, §6 route types).
  - Fixtures in `backend/testdata/gtfsrt/` (1.6 MB total): empty/stale feed + 4 live samples trimmed to 800 m around U Wedding.
- Data facts found (most important):
  - The configured static URL is stale (2026-08-21); `https://www.vbb.de/gtfs` is current and matches GTFS-RT 100 % (vs 46.9 %). → open question #4.
  - GTFS-RT rate limit is 7/min since 2026-11-02. → open question #5. Provider also asks for compressed responses and a User-Agent with contact info (for the Phase 4 poller).
  - GTFS-RT was empty with a 9-day-old header for ≥ 12 min; freshness must come from `header.timestamp`.
  - Stations must be grouped by `parent_station` or the DHID prefix: most bus poles have no parent.
  - RT sends absolute times (delay only on 24 %), TripUpdates only, sparse for BVG U-Bahn/tram/bus.
- Tests: exploration only (task allows "script/test, not shipped"). `go vet`, `gofmt`, golangci-lint 0 issues. Re-running `rt` on the trimmed fixtures reproduces the match rates (100 % trips, 99.7 % stops).
- Requests to VBB: 2 static downloads, ~20 GTFS-RT requests over 75 min (all ≤ 7/min).
- New deps (+ why): `github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs` v1.0.0 (in stack; decode GTFS-RT protobuf; also used by the Phase 4 poller), `google.golang.org/protobuf` (now direct; marshal trimmed fixtures).
- Follow-up (user decision, same day): `DefaultGTFSURL` → `https://www.vbb.de/gtfs`; `MinGTFSRTPollInterval` 5 s → 10 s (+ boundary tests 9 s rejected / 10 s accepted); CLAUDE.md rule 7, ARCHITECTURE §2/§3.1/§3.3/§3.7, PLAN 4.1, `.env.example` updated (7 req/min, gzip, User-Agent, freshness from `header.timestamp`).
- Follow-up 2026-11-09 (browser research): VBB's open-data page links `https://unternehmen.vbb.de/gtfs` (same file as www.vbb.de/gtfs, "2x wöchentlich") → made it the default. Also found: official line colors CSV, a staging GTFS-RT feed, and the feed's source code README: events with `time` but no `delay` are filled from the schedule, so only `delay` marks realtime (DATA_MODEL §7 #18). Second RT sample (23:27 UTC): bus share 36 %, U-Bahn/tram still ~1–3 %.
- Plan update 2026-11-09 (user approved): PLAN 1.2, 1.4, 1.5, 1.8, 2.2, 2.4, 4.0 (new), 4.1–4.4 adjusted to the findings; DATA_MODEL §3.2 loop + `is_ring` rule, new §3.4 `stations`; ARCHITECTURE §3.4 realtime rule. Order of phases unchanged.
- Next: 1.2.

### 2026-11-08 — task 0.10
- Done:
  - CLAUDE.md §5 rewritten: every command runs from the repo root (`go -C backend …`, `npm --prefix mobile …`), so each line can be pasted on its own. Covers Compose up/health/logs/down, `go vet`, full and `-short` tests, golangci-lint via Docker (v2.15.0, same as CI), `go generate` + `gen:api` (with "commit the generated files"), `npm ci`/typecheck/lint/test/start, `start -- --clear`, `expo install`, and the Phase 1.7 import trigger (marked as not implemented). Notes on Docker requirements, `-race`, phone setup.
  - `curl.exe` instead of `curl` (in Windows PowerShell 5.1 `curl` is an alias of Invoke-WebRequest).
  - `tests/node/core-boundary.test.ts`: ESLint warm-up moved to `beforeAll` (60 s timeout). The first lint loads all Expo plugins and took 7.8 s on a busy machine → the first case hit Vitest's 5 s default and failed. Would have been flaky in CI too.
- Tests (acceptance): ran every command verbatim in Windows PowerShell 5.1.26100 from the repo root: compose up `--wait` → healthy, health → `ok`, logs, vet, test (full + `-short`), golangci-lint 0 issues, `go generate` + `gen:api` → no diff, `npm ci` (on a scratch clone, because Expo was running in `mobile/`), typecheck, lint, test (Vitest 33 + Jest 7), `start -- --clear --help` (arg passing through npm.ps1 works), `cd mobile; npx expo install --check; cd ..`. Not run: `compose down`, `logs -f` and `start` without `--help` (long-running / would stop the running stack).
- New deps (+ why): none.
- Phase 0 status: all tasks done except 0.9 (blocked by the GitHub billing lock, see below).
- Next: Phase 1, task 1.1 (data exploration), once the user agrees to start it while 0.9 is blocked.

### 2026-11-08 — task 0.9
- Done:
  - `.github/workflows/ci.yml` (on push to master and on every PR; `contents: read`; concurrency cancels superseded runs). Actions pinned to commit SHAs with the release in a comment: checkout v7.0.1, setup-go v7.0.0, setup-node v7.1.0, golangci-lint-action v9.3.0 (all released ≥ 4 weeks ago).
    - `backend`: Go from `backend/go.mod`, `go vet ./...`, golangci-lint v2.15.0 (same version as local), `go test -race -count=1 ./...` (testcontainers uses the runner's Docker; `-race` needs cgo, which the Windows dev machine lacks → first race-detector run happens here).
    - `mobile`: Node from `mobile/.nvmrc` (26, same major as local), `npm ci`, `typecheck`, `lint`, `test`.
    - `contract`: `go generate ./...` (oapi-codegen + sqlc in Docker) and `npm run gen:api`, then fails with a hint and the diff if `git status --porcelain` is not empty.
  - `backend/.golangci.yml` (v2): standard linters + `gofmt` formatter, so unformatted Go fails CI. Generated code excluded (`lax`).
- Tests (local simulation): `golangci-lint config verify` OK, `run` → 0 issues; a deliberately mis-formatted file is reported by `gofmt`. `actionlint` (incl. shellcheck) clean. Fresh `git clone` (no `.env`, `.expo`, `node_modules`) → `npm ci`, typecheck, lint, Vitest 33 + Jest 7 green; regenerate everything → no diff.
- New deps (+ why): none. CI only.
- PR run (2026-11-08): https://github.com/256-byte/punktchen/pull/1 → all three jobs failed after 2 s without running a step. Annotation: "The job was not started because your account is locked due to a billing issue." Not a workflow problem; the user is resolving it with GitHub support.
- Decision (user, 2026-11-08): merge PR #1 anyway (CI config only, simulated locally), mark 0.9 `blocked`, continue with 0.10. 0.9 is done when a PR run is green after the account is unlocked (re-run with `gh run rerun`).
- Next: 0.10.

### 2026-11-08 — task 0.8
- Done:
  - `EXPO_PUBLIC_API_URL` from `mobile/.env` (template `mobile/.env.example`; `.env` is git-ignored). Read once in `src/lib/api.ts` as a literal `process.env.EXPO_PUBLIC_API_URL` (Expo inlines it at bundle time) and validated by `src/core/apiBaseUrl.ts` (pure: http(s) only, no query/fragment/credentials, port 1–65535, trailing slashes removed; regex instead of `new URL()` because RN's URL polyfill is incomplete).
  - `src/api/client.ts`: openapi-fetch client typed by the generated `paths`; 5 s timeout via AbortController (an unreachable LAN IP otherwise hangs ~1 min on iOS) that also honours the caller's abort signal; errors become `ApiError`. `fetchHealth()`.
  - `src/api/health.ts`: `useHealth()` (TanStack Query, no retry). `app/_layout.tsx` provides a `QueryClient` (persistence comes with Phase 3/5).
  - `src/ui/BackendStatus.tsx` on the start screen: "Backend: ok/degraded/down" + version + server time + URL; "Backend unreachable" with the error and a network hint; "Backend not configured" when the env var is missing/invalid; "Check again" button.
  - README "Run the app on your phone": LAN IP via `ipconfig`, `mobile/.env`, Android emulator `10.0.2.2`, same Wi-Fi (no guest network), firewall (Docker Desktop's inbound rule; fallback `New-NetFirewallRule` for 8080 on the Private profile), Expo account + Windows login workarounds, `expo start --clear` after env changes.
- Findings:
  - Docker Desktop already has an inbound allow rule for `com.docker.backend.exe` (Public profile), which is what accepts the published port 8080 → no extra firewall rule was needed on this PC. The Wi-Fi is classified as "Public" by Windows; the README recommends "Private".
  - `npm audit`: 31 moderate/low advisories, all in transitive build/test tooling (e.g. `minimatch`, `glob`, `deepmerge` via Expo CLI / Jest). `npm audit fix --force` would break the SDK-pinned versions; left as is, revisit on the next Expo SDK upgrade.
  - Typecheck does not depend on the git-ignored `expo-env.d.ts` / `.expo/` (checked with a tsconfig that excludes them), so CI can run it on a fresh clone.
- Tests: Vitest 33 (20 new for `parseApiBaseUrl`: valid/normalized URLs, missing value, 8 invalid forms, port range). Jest 7 (6 new for `BackendStatus` with a fake fetch: ok + requested URL, down, unreachable + hint, timeout, "Check again" refetch, missing config without network). `typecheck`, `lint` green. `expo export --platform ios` with `.env` → bundle contains the configured URL.
- New deps (+ why): `openapi-fetch` (stack; typed client from the generated types), `@tanstack/react-query` (stack; request state now, cache/persistence later).
  - A Metro dev server that was already running while packages were installed fails with "Unable to resolve module ./useQueries.js from …/@tanstack/react-query/…" (its file map missed the new files). Fix: stop it and run `npx expo start --clear`.
- Manual check (acceptance): iPhone in Expo Go on the home Wi-Fi shows "Backend: ok" with version and server time from `http://192.168.178.29:8080/v1/health` (2026-11-08).
- Next: 0.9 CI.

### 2026-11-07 — task 0.7
- Done:
  - `mobile/`: Expo SDK 58 (expo 58.0.23, React Native 0.88.1, React 19.3.0, TypeScript 6.0), Expo Router with routes in `mobile/app/` (CLAUDE.md §4; the SDK 58 template would use `src/app/`). Placeholder `app/index.tsx` + Stack layout. iOS and Android only (`platforms`), typed routes on.
  - Started from `create-expo-app --template default` in a scratch dir and kept only what is needed: no example screens, no Expo-branded assets/icons (app icon comes in Phase 7), no web target, no Reanimated/Gesture Handler/bottom-sheet yet (added in the phase that uses them).
  - `tsconfig.json`: Expo base + `strict`, `noUncheckedIndexedAccess`, `noFallthroughCasesInSwitch`, alias `@/*` → `src/*`.
  - ESLint 9 flat config: `eslint-config-expo` + `eslint-config-prettier`; `no-restricted-imports` for `src/core/**` forbids `react`, `react-native`, `expo` (and subpaths), `react-native-*`, `expo-*`, `@expo/*`, `@react-native/*`, `@react-navigation/*`.
  - Prettier (single quotes, width 100). `npm run lint` = ESLint + `prettier --check`.
  - Vitest for `src/core` + `tests/node`; Jest (jest-expo preset) + RNTL 14 for UI, ignoring those two dirs. `npm test` runs both.
  - `npm run gen:api`: openapi-typescript → `src/api/schema.gen.ts` (marked generated in `.gitattributes`).
  - ARCHITECTURE §4.6 (tooling and tests).
- Findings:
  - TypeScript 6 no longer loads `@types/*` automatically (`types` defaults to `[]`). Jest tests import `describe/it/expect` from `@jest/globals` (same style as Vitest); `@types/jest` not needed.
  - openapi-typescript 7.13.0 (latest) declares `peer typescript ^5.x`; Expo SDK 58 ships TS 6. npm `overrides` lets it use the project's TS 6; output is byte-identical across runs and typechecks.
  - npm resolved the optional `react-dom` peer (via expo-router's web deps) to 19.3.1, which needs react 19.3.1 → ERESOLVE. Pinned `react-dom` 19.3.0 like the Expo template does.
  - npm 11 blocks install scripts by default. `@parcel/watcher` (via jest-haste-map) wants `node-gyp rebuild` as a fallback; the prebuilt `@parcel/watcher-win32-x64` is installed, so the script is denied in `allowScripts`.
- Tests: `npm run typecheck`, `npm run lint`, `npm test` green. Vitest: 13 cases for the core boundary rule (9 forbidden imports rejected, 3 allowed, rule inactive outside `src/core`) run against the real ESLint config. Jest: HomeScreen renders. `gen:api` twice → identical sha256. `npx expo export --platform ios` bundles (Hermes, 2.5 MB). `npx expo-doctor` 20/20 checks passed.
- New deps (+ why):
  - runtime: `expo`, `expo-router`, `react`, `react-native` (stack); `expo-constants`, `expo-linking`, `@expo/metro-runtime`, `react-native-screens`, `react-native-safe-area-context` (required peers of expo-router); `react-dom` (peer pin, see findings).
  - dev: `typescript`, `@types/react`, `eslint`, `eslint-config-expo`, `eslint-config-prettier`, `prettier` (lint/format); `vitest` (core tests); `jest`, `jest-expo`, `@jest/globals`, `@testing-library/react-native`, `test-renderer` (UI tests; test-renderer is RNTL 14's required peer); `openapi-typescript` (API types).
  - Expo Go (SDK 58) only opens a project when the same Expo account is signed in both in Expo Go and in Expo CLI ("You need to be signed in to Expo Go and Expo CLI"). A free account is enough.
  - `npx expo login` (browser flow) crashes on Windows: the CLI passes the login URL unquoted to `cmd /c start`, so the `&` in the URL splits the command. Workarounds: `npx expo login --no-browser` (accounts with a password), or an access token in `EXPO_TOKEN` (works for Google/GitHub sign-up accounts). Never commit the token.
  - Metro picked the right LAN address (WLAN `192.168.178.29`, not the VMware adapters). Windows Firewall already had an inbound allow rule for Node.js.
- Manual check (acceptance): `npx expo start` on the PC, QR code scanned on the iPhone → app opens in Expo Go and shows "LiveStop / Live departures around you" (2026-11-08).
- Next: 0.8 phone → backend networking (README section on LAN IP + firewall can reuse the findings above).

### 2026-11-07 — task 0.6
- Done:
  - `backend/sqlc.yaml`: PostgreSQL engine, schema = `db/migrations` (sqlc applies the golang-migrate `*.up.sql` files in order, ignores `*.down.sql`), queries = `db/queries`, output `gen/queries` (package `queries`, pgx/v5). Nullable columns → `*T`, `timestamptz` → `time.Time`, `jsonb` → `json.RawMessage`, `url`/`sha256` renamed to `URL`/`SHA256`, strict function checks on.
  - First query `db/queries/feed_imports.sql`: `LastSuccessfulFeedImport(source)` → newest `succeeded` row of `meta.feed_imports` (input for change detection in Phase 1; no importer logic added).
  - `db/generate.go`: `//go:generate go run ../internal/tools/sqlc generate`. `go generate ./...` now runs oapi-codegen and sqlc.
  - `internal/tools/sqlc`: small wrapper that runs `docker run sqlc/sqlc:1.31.1@sha256:70f5…5537` with the backend dir mounted (on Linux as the calling user, so CI-generated files are not owned by root); forwards sqlc's exit code.
- Why sqlc in Docker and not as a `go tool` like oapi-codegen: sqlc's PostgreSQL parser (`pg_query_go`) needs cgo + a C toolchain, which the Windows dev machine does not have. Docker is already required (Compose, testcontainers); pinning by digest makes output identical locally and in CI.
- Tests: integration test `db/queries_test.go` (migrated PostGIS container): `pgx.ErrNoRows` on empty table; with 5 rows picks the newest succeeded row of the right source (ignores newer failed/running rows and other feeds) and maps every type correctly. Unit test for the wrapper's root lookup (caught: relative path was returned; now always absolute). `go generate ./...` twice → identical hashes. Broken query (unknown column) → `go generate` fails with sqlc's error message. `go vet` / `gofmt` / `golangci-lint` clean, full `go test ./...` green.
- New deps (+ why): none in go.mod. Tool: Docker image `sqlc/sqlc:1.31.1` (in stack).
- Note for 0.9 / 0.10: CI contract job and CLAUDE.md §5 must state that `go generate ./...` needs Docker.
- Next: 0.7 Expo app (Node.js must be installed on the dev machine first).

### 2026-11-06 — task 0.5
- Done:
  - `api/openapi.yaml` (OpenAPI 3.1.0) v0: `GET /v1/health`, `/v1/layers`, `/v1/nearby`, `/v1/stations/{stationId}` with schemas from DATA_MODEL §5 (`Health`, `LayerInfo`, `NearbyResponse`, `ResponseMeta`, `LayerMeta`, `TransitLayer`, `Station`, `Pole`, `Direction`, `Departure`, `Mode`, `Status`, `Error`). Nullable fields use 3.1 `type: [X, "null"]`. ETag/Cache-Control headers, `If-None-Match`, `304`, `400`, `404` and a `default` Error response on every operation.
  - Codegen: oapi-codegen v2.8.2 as a Go tool dependency (`tool` directive in go.mod, run via `go tool`), config `backend/internal/api/oapi-codegen.yaml` (models + std-http-server + strict-server, prefixed enum names), `//go:generate` in `internal/api/generate.go`, output `backend/gen/openapi/openapi.gen.go`.
  - `internal/api`: `Server` implements the generated `StrictServerInterface`. `/v1/health` → `{status, version, time}`; `down` if the DB ping (1 s timeout) fails. The other three return 501 `not_implemented` via the `default` response. Param binding errors → 400 `invalid_argument`, unknown path → 404 `not_found`, handler errors → 500 `internal` (all in the `Error` format).
  - `platform.Berlin` (Europe/Berlin, tzdata embedded via `time/tzdata`) for API timestamps.
  - Placeholder handler removed from `cmd/server`.
  - ARCHITECTURE §3.7: health always 200, Docker healthcheck = liveness, Phase 0 status rule.
- Contract decisions (within ADR-0003/0008, no new ADR):
  - `NearbyResponse.layers` is an object with one optional, typed property per layer (`transit: TransitLayer`), each payload with a `layer` discriminator field, instead of a `oneOf` union map. Same JSON as DATA_MODEL §5.2, simpler generated Go/TS types, new layers are additive.
  - `Station.distanceMeters` is optional (only meaningful on `/v1/nearby`); `ResponseMeta.cell` likewise.
  - `Error.code` is a string with documented values, not an enum, so adding codes never breaks clients.
  - Temporary 501s use the `default` response instead of a dedicated `501` in the contract.
- Tests: `internal/api` unit tests with fake DB pinger + fixed clock: health ok (time `2026-11-05T14:32:00+01:00`, whole seconds), health with DB down (200 + `down`), 501 JSON for 4 paths, 400 JSON for 3 bad param cases, 404 JSON. `go generate ./...` twice → identical output. `go vet` / `gofmt` / `golangci-lint` clean, full `go test ./...` green.
- Manual check (acceptance): `curl localhost:8080/v1/health` → `200 {"status":"ok","time":"2026-11-06T17:27:58+01:00","version":"dev"}`. `docker compose stop db` → `down`; start db → `ok` again (pool reconnects).
- New deps (+ why): `github.com/oapi-codegen/oapi-codegen/v2` v2.8.2 (tool, in stack; OpenAPI 3.1 support since v2.8.0, "initial"), `github.com/oapi-codegen/runtime` v1.8.0 (imported by generated code for param binding).
- Not done here (by plan): request validation against the spec (lat/lng ranges, radius 100–1000, unknown layer → 400) and contract tests → task 2.5. TypeScript generation → 0.7.
- Next: 0.6 sqlc.

### 2026-11-06 — task 0.4
- Done:
  - `backend/db/migrations/0001_meta.{up,down}.sql`: `CREATE EXTENSION IF NOT EXISTS postgis`, schema `meta`, table `meta.feed_imports` with CHECK constraints (status values, `finished_at` ⇔ not running, `error` only on failure, sha256 hex) + index `(source, started_at DESC)`. Down drops only the table (schema `meta` holds the migration table; postgis may be used by gtfs data).
  - `backend/db` package embeds the migrations (`//go:embed`), so the binary/image needs no extra files.
  - `platform.Migrate`: golang-migrate on top of the app's pgx pool (`stdlib.OpenDBFromPool`, pgx5 driver, iofs source). Version table in `meta.schema_migrations` (creates schema `meta` first, the driver does not). `ErrNoChange` → "up to date". Dirty DB → startup fails with a clear message. Stops between migrations on SIGTERM. golang-migrate logs go through slog (`component=migrate`).
  - `cmd/server`: runs migrations after connecting, before opening ports (so "healthy" implies a migrated schema).
  - DATA_MODEL §3.3: status values and constraints of `feed_imports` documented.
- Tests: `go test ./...` green, `go vet` / `gofmt` / `golangci-lint` clean. Unit: migration file conventions (names, versions 1..n without gaps, every up has a down). Integration (testcontainers): apply twice → no error, version 1 not dirty, table in `meta` not `public`, postgis installed; `feed_imports` constraints (4 valid, 5 invalid rows); down then up; a broken migration is fully rolled back (verifies the implicit-transaction claim) and the next run fails with "dirty".
- Manual check (acceptance): existing dev volume → first start logs `1/u meta` and `from_version 0 → to_version 1`; `docker compose restart backend` → `database schema up to date, version 1`; container healthy.
- New deps (+ why): `github.com/golang-migrate/migrate/v4` v4.20.1 (in stack; pgx v5 driver + iofs source).
- Findings: Postgres runs a multi-statement query as one implicit transaction → migration files must not contain statements that cannot run in a transaction (e.g. `CREATE INDEX CONCURRENTLY`); noted in `db/db.go`. Each DB integration test starts its own container (~4–5 s each, platform package now ~40 s). If this grows, switch `pgtest` to one shared container with a fresh database per test.
- Next: 0.5 OpenAPI v0 + health.

### 2026-11-06 — task 0.3
- Done:
  - `internal/config`: `Load(lookup)` reads all `LIVESTOP_*` vars, applies defaults (same as `.env.example`), validates every value and returns **all** errors at once. `LIVESTOP_DATABASE_URL` is required. `Secret` type keeps the DB URL out of `fmt`/`slog` output; `Config.LogValue()` is the startup summary with the password redacted (`xxxxx`). HTTP and metrics addr must differ. GTFS-RT poll interval floor: 5 s (`MinGTFSRTPollInterval`, safety margin below VBB's 60 req/min).
  - `internal/geo`: `BBox` + `ParseBBox` (for `LIVESTOP_SCOPE_BBOX`).
  - `internal/platform`: `NewLogger` (slog JSON), `NewPool` (pgxpool, `application_name=livestop`, ping with 10 s timeout → fail fast), `NewMetricsRegistry` + `MetricsHandler` (own registry, Go + process collectors), `Listen` (binds early → port conflicts fail at startup) + `Serve` (runs API and metrics servers, graceful shutdown on ctx cancel, 8 s timeout, then force close).
  - `internal/platform/pgtest`: starts a throwaway PostGIS container via testcontainers (skipped with `-short`). Reused by DB tests from 0.4 on.
  - `cmd/server`: config → logger → DB → listeners → serve; SIGINT/SIGTERM via `signal.NotifyContext`; `version` via `-ldflags -X main.version` (Dockerfile `ARG VERSION`). Health handler is still a temporary stub in `main.go` (replaced in 0.5).
- Tests: `go test ./...` green. Unit: config (defaults, all set, empty = default, 14 invalid values, all errors reported, secret never printed via slog/fmt/error), bbox parsing, logger JSON + level filter, metrics output, port-in-use, graceful shutdown (in-flight request finishes, `Serve` returns nil), shutdown timeout. Integration (testcontainers, PostGIS 18-3.7): pool connects, `application_name` set; unreachable DB fails fast without leaking the password. `go vet`, `gofmt`, `golangci-lint` 2.15 (via Docker image) → 0 issues.
- Manual check (acceptance): `docker compose up -d --build --wait` → healthy; log shows config summary with `postgres://livestop:xxxxx@db:5432/...`; `docker compose stop backend` → logs `shutting down` (reason `terminated signal received`) and `stopped`, exit code 0. On the host: bad config → one error line listing every bad var, exit 1; DB down → immediate error, exit 1.
- New deps (+ why): `github.com/jackc/pgx/v5` v5.12.0 (DB pool, in stack), `github.com/prometheus/client_golang` v1.25.0 (metrics, in stack), `github.com/testcontainers/testcontainers-go` + `modules/postgres` v0.45.0 (DB integration tests, in stack; test-only).
- Machine notes: `go test -race` needs cgo/gcc, not available on this Windows PC → race detector runs in CI (0.9). `golangci-lint` is not installed locally; ran it with `docker run golangci/golangci-lint` (to be put into CLAUDE.md §5 in 0.10).
- Next: 0.4 Migrations.

### 2026-11-06 — task 0.2
- Done: `docker-compose.yml` with `db` (`postgis/postgis:18-3.7`, volume `pgdata`, `pg_isready` healthcheck, `shm_size: 512mb`) and `backend` (multi-stage `backend/Dockerfile`: `golang:1.27-alpine` → `gcr.io/distroless/static-debian13:nonroot`, static binary, 15.8 MB image). `.env.example` with every `LIVESTOP_*` var. README "How to run" for the backend.
- Ordering note: 0.2 needs a backend binary to be "healthy", but the Go skeleton is 0.3. Added a stdlib-only placeholder `backend/cmd/server/main.go` (`GET /v1/health` → `{"status":"ok"}`, SIGTERM shutdown) and `backend/go.mod` (module `github.com/256-byte/punktchen/backend`, go 1.27). 0.3 replaces the placeholder; 0.5 moves `/v1/health` behind generated handlers.
- Healthcheck: distroless has no shell/curl, so the binary has a `healthcheck` subcommand (`/livestop healthcheck`), used by the Dockerfile `HEALTHCHECK`.
- Config vars introduced (to be parsed + validated in 0.3): `LIVESTOP_DB_USER|PASSWORD|NAME|HOST_PORT` (compose only), `LIVESTOP_DATABASE_URL`, `LIVESTOP_HTTP_ADDR`, `LIVESTOP_METRICS_ADDR`, `LIVESTOP_LOG_LEVEL`, `LIVESTOP_DEV`, `LIVESTOP_GTFS_URL`, `LIVESTOP_GTFS_IMPORT_TIME`, `LIVESTOP_SCOPE_BBOX`, `LIVESTOP_GTFSRT_URL`, `LIVESTOP_GTFSRT_POLL_INTERVAL`. In compose, `DATABASE_URL`/`HTTP_ADDR`/`METRICS_ADDR` are set by the compose file and override `.env`.
- Ports: 8080 on all interfaces (phone over LAN), 9090 and 5432 on 127.0.0.1 only.
- Tests (manual check): `docker compose up -d --build --wait` → both `healthy`. `curl localhost:8080/v1/health` → 200 `{"status":"ok"}`. Postgres 18.6, PostGIS 3.7.0 available, data dir `/var/lib/postgresql/18/docker` (inside the volume). Row written → `down` (1 s, clean SIGTERM) → `up` → row still there. Container runs as `nonroot`. `go vet` + `gofmt` clean.
- New deps: none (Go stdlib only). Images: `postgis/postgis`, `golang`, `distroless/static`.
- Infra facts: Postgres 18 images moved PGDATA to `/var/lib/postgresql/<major>/docker` → volume mounted at `/var/lib/postgresql`. The postgis image already creates the `postgis` extension in `POSTGRES_DB`; migration 0001 (task 0.4) should still `CREATE EXTENSION IF NOT EXISTS` so it does not depend on the image.
- Machine note: Node.js is not installed on the dev PC → needed before 0.7.
- Next: 0.3 Go skeleton.

### 2026-11-05 — task 0.1
- Done: directory tree from CLAUDE.md §4 (empty dirs hold a `.gitkeep`), `.gitignore` (secrets, downloaded data, Go/Node/Expo build output, editor/OS files; `backend/testdata/**` re-included), `.editorconfig`, `.gitattributes`, MIT `LICENSE`, `README.md` (what / why / layout / run status).
- Extra (not in task text): `.gitattributes` with `eol=lf`, because this machine has `core.autocrlf=true`. Without it gofmt output, container shell scripts and the CI drift check (0.9) would break on CRLF.
- Tests (manual check): script compared every dir of CLAUDE.md §4 against the tree → none missing. `git check-ignore` → `.env`, `*.zip`, `*.pb`, `*.sql.gz`, `/data/`, `/backend/data/`, `node_modules/`, `.expo/`, `backend/bin/` ignored; `.env.example`, `backend/testdata/*.zip|*.pb`, `.vscode/extensions.json` not ignored.
- New deps: none.
- Data facts found: none.
- Note for 0.7: `mobile/` is not empty (`.gitkeep` files), so `create-expo-app` may refuse it. Scaffold in a temp dir and move the files in.
- Next: 0.2 Docker Compose.
