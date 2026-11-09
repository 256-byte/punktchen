# Data Model

> Source of truth for data structures. If code and this document disagree, fix one of them in the same change.
> Anything marked **(verify)** is an assumption that must be checked against real data in Phase 1 and moved to §7.

## 1. Glossary

| Term | Meaning |
|---|---|
| **GTFS** | General Transit Feed Specification. Zip of CSV files describing the schedule. |
| **GTFS-RT** | GTFS Realtime. Protobuf feed with delays/cancellations (TripUpdates), positions, alerts. |
| **VDV** | German transport association standards (VDV 452/453/454). Upstream source of VBB realtime; we never consume VDV directly. |
| **Station** | Group of poles, e.g. "S+U Wedding". `parent_station` if set, else the DHID prefix `de:<AGS>:<station>` of the pole id (§7 #4). |
| **Pole** (Mast) | A physical stop point / platform (`location_type = 0` or empty) with its own coordinates. "Mastscharf" = data at pole precision. |
| **Direction** | `(route, direction_id, headsign)` departing from a pole. |
| **Service day** | The operating day a trip belongs to. Times ≥ 24:00:00 belong to the previous calendar date. |
| **Trip instance** | `(trip_id, service_date)`. The unit realtime updates refer to. |
| **Snapshot** | Immutable in-memory merge of schedule + realtime, versioned. |
| **Layer** | A kind of map content (transit, later toilets, alerts...). |
| **Feature** | One item of a layer on the map (a station, a toilet...). |

## 2. Imported GTFS tables (schema `gtfs`)

Only the columns we use are listed; import all columns of these files anyway (cheap, useful for debugging).

| Table | Key columns we use |
|---|---|
| `agency` | `agency_id`, `agency_name` |
| `routes` | `route_id`, `agency_id`, `route_short_name`, `route_type`, `route_color`, `route_text_color` |
| `trips` | `trip_id`, `route_id`, `service_id`, `trip_headsign`, `direction_id`, `shape_id` |
| `stop_times` | `trip_id`, `stop_sequence`, `stop_id`, `arrival_time`, `departure_time`, `pickup_type`, `drop_off_type` |
| `stops` | `stop_id`, `stop_name`, `stop_lat`, `stop_lon`, `location_type`, `parent_station`, `platform_code` |
| `calendar` | `service_id`, `monday..sunday`, `start_date`, `end_date` |
| `calendar_dates` | `service_id`, `date`, `exception_type` |
| `shapes` | import later (Phase 7+), not needed for departures |

Storage notes:
- DDL: `backend/db/gtfs/schema.sql` (read by sqlc; the importer runs it with `gtfs.` → `gtfs_staging.`). All GTFS-reference columns of these files are created; empty CSV values are NULL.
- `arrival_time` / `departure_time` stored as **integer seconds since service-day start** (`arrival_secs`, `departure_secs`), because values like `25:10:00` exist. Keep the raw text column too.
- `stops.geom geography(Point, 4326)` + GiST index.
- Extra column `stops.in_scope boolean` (scope filter, §4).
- Keys and indexes (`backend/db/gtfs/indexes.sql`, built after COPY): primary keys on routes, trips, stops, calendar, calendar_dates `(service_id, date)`, stop_times `(trip_id, stop_sequence)` (duplicates fail the import); indexes `stop_times(stop_id, departure_secs)`, `stop_times(departure_secs)` (schedule window, task 2.2), `trips(service_id)`, `trips(route_id)`, `stops(parent_station)`, GiST on `stops.geom`. No foreign keys (dangling references are a sanity check, task 1.6).

## 3. Derived tables (built after each import, schema `gtfs`)

All derived tables are built by `backend/db/gtfs/derived.sql` in the import transaction (task 1.5). They depend only on the feed, never on the import date.

### 3.1 `service_dates`
Every date on which a service runs, expanded over the feed's **whole validity** (PK `(service_date, service_id)`).

| column | type |
|---|---|
| `service_date` | date |
| `service_id` | text |

Built from `calendar` (weekday + range) plus `calendar_dates` (1 = add, 2 = remove). Changed in 1.5 from "`[today − 1, today + 7]`, rebuilt daily": the whole range is only ~257,000 rows (§7 #22), needs no daily rebuild and keeps imports deterministic.

### 3.2 `pole_directions`
What leaves from each pole. Used for markers and for showing directions with no departure in the window.

| column | type | notes |
|---|---|---|
| `stop_id` | text | pole |
| `station_id` | text | `stops.station_id` (§3.4) |
| `route_id` | text | |
| `line` | text | `route_short_name`, e.g. `U6`, `S1`, `M27` |
| `mode` | text | mapped from `route_type`, §6 (the mapping lives in derived.sql) |
| `direction_id` | smallint | always 0/1 in VBB data (§7 #8); nullable column anyway (optional in GTFS) |
| `headsign` | text | `trip_headsign` (last stop's name if empty), the grouping key, e.g. `U Alt-Tegel (Berlin)` |
| `display_headsign` | text | what riders see at this pole: the most common `coalesce(stop_headsign, trip_headsign)` of the group (§7 #21) |
| `is_terminal` | bool | pole is the last stop for ≥ 95% of the **non-loop** trips of this (route, direction, headsign) at this pole |
| `is_origin` | bool | same with the first stop |
| `is_ring` | bool | S-Bahn line S41/S42 or headsign starting with "Ringbahn" (§7 #11) |
| `trip_count` | bigint | trip instances (trip × service date) over the feed's validity, for ordering |

One row per (pole, route, direction, headsign) that has stop times; unserved poles have none (§7 #12). Indexes on `stop_id` and `station_id`.

Terminal rule: at a terminal pole, arrivals that end there are **not** shown as departures; only the opposite direction is shown.

**API directions of a pole** (task 2.4, `transit.buildPole`; user decision 2026-11-09). Rows are many more than what riders see: at the Wedding area (500 m) 174 rows at 23 poles, mostly short-turn and night variants with no departure in the window (§7 #26). The API applies, in order:
1. **Terminal:** a row with `is_terminal` and no departure in the window is left out (a rare continuing trip keeps it, flagged `isTerminal`).
2. **Merge:** rows with the same label — mode, line, display headsign (without " (Berlin)"), ring flag — are one direction; departures are combined, sorted, cut to the limit. Example: S41 "Ringbahn S 41" from 10+ trip headsigns at one platform.
3. **Trim:** per line and direction of travel (mode, line, `direction_id` of the label's most frequent row), only directions with departures are listed. If none has one, the most frequent (`trip_count`) is listed with an empty list, so a pole still shows which lines stop there (e.g. night lines in the afternoon). This hides empty variants like "U6 → U Seestr." while the U6 runs. Lines suspended for weeks stay listed this way too (e.g. RE2 at Alexanderplatz before 2026-12-13, §7 #27); user decision 2026-11-09. The app keeps these empty directions off the map markers and lists them last in the station sheet (user decision 2026-11-09, after seeing 72 of 151 directions empty around Alexanderplatz at 18:19).

Poles left without directions and stations left without poles are not returned. Wedding 500 m at 17:00: 174 rows → 51 directions.

Loop trips (first station = last station, e.g. Ringbahn full circles, bus 365; §7 #11) never make a pole terminal: at their start and end station they are departures, not arrivals.

### 3.3 `meta` schema (not swapped)

- `meta.feed_imports(id, source, url, etag, last_modified, sha256, started_at, finished_at, status, row_counts jsonb, error)` — migration `0001_meta`
  - `status`: `running` | `succeeded` | `failed` | `skipped` (unchanged upstream).
  - `finished_at` is NULL exactly while `running`; `error` only on `failed`; `sha256` is lowercase hex or NULL (nothing downloaded).
  - `etag` / `last_modified` are stored as raw header text, to send back unchanged in conditional GETs.
  - `row_counts`: rows per table (loaded and derived) plus `in_scope_poles` and `in_scope_stations`; the next import compares against them (sanity check "drop > 50 %").
  - Lifecycle (task 1.2, `internal/transit/static`): a `running` row is opened **before** the download (failed downloads are logged too) → validators + `sha256` are recorded after the download → `skipped` if upstream answered 304 or the hash equals the last **succeeded** row of the same `source`, else the importer closes it as `succeeded` (with `row_counts`) or `failed`. Exception: if the live `gtfs` schema is missing or its layout stamp differs from the binary's (ARCHITECTURE §3.1), the download is forced: no conditional headers, no hash comparison. Rows are closed even when the caller's context is cancelled. Source name of the static feed: `vbb-gtfs`.
- `meta.schema_migrations` (golang-migrate, version + dirty flag). Migrations are embedded in the binary (`backend/db/migrations`) and run on every startup.

### 3.4 `stations`
The station of every pole, because most bus/tram poles have no `parent_station` (§7 #4). The API's `Station` is built from this table.

| column | type | notes |
|---|---|---|
| `station_id` | text | `parent_station`, else the DHID prefix `de:<AGS>:<station>` of the pole id, else the pole id itself |
| `name` | text | the `location_type=1` row's name if it exists, else the most frequent name among its poles |
| `geom` | geography(Point, 4326) | the `location_type=1` row's coordinates if it exists, else the centroid of its poles |
| `modes` | text[] | distinct modes of all directions at its poles, sorted; `{}` if none is served |
| `in_scope` | bool | any of its poles is in scope (§4) |
| `pole_count` | int | poles of the station, served or not |

PK `station_id`, GiST on `geom`. Every pole (`location_type` 0/empty) also gets the column **`stops.station_id`** (decided in 1.5: a column, not a separate mapping table), indexed.

### 3.5 `trip_info`
One row per trip (PK `trip_id`). Kept after the import since task 2.2 (before: a temp table inside derived.sql): `pole_directions` is built from it, and the schedule window needs each trip's service and last stop without aggregating `stop_times` every minute.

| column | type | notes |
|---|---|---|
| `trip_id` | text | |
| `service_id`, `route_id`, `direction_id` | | copied from `trips` |
| `headsign` | text | `trip_headsign`, else the last stop's name: the direction key of `pole_directions` |
| `first_stop_sequence`, `last_stop_sequence` | integer | the last stop is an arrival, never a departure |
| `is_loop` | bool | first and last stop in the same station (§7 #11); NULL if an end has no station |
| `instances` | bigint | service dates of the trip over the feed's validity |

### 3.6 Schedule window (task 2.2, not a table)
A **departure** is a stop time where riders can board: at an in-scope pole, `pickup_type` ≠ 1, not the trip's last stop, on a service date its service runs. Absolute time = service day's noon − 12 h (Europe/Berlin) + `departure_secs`; on DST switch days this start is 23:00 (spring) or 01:00 (autumn) local time, so night times shift against the wall clock (`departures.ServiceDay`, tests in `serviceday_test.go`).

## 4. Scope

- Import the **full VBB feed**; restrict by `stops.in_scope`.
- Default scope: Berlin bounding box `13.088,52.338,13.761,52.675` (minLon,minLat,maxLon,maxLat), config `LIVESTOP_SCOPE_BBOX`. Can later become a polygon.
- A pole (`location_type` 0/empty) is in scope if its coordinates are inside; a `location_type=1` station if any child pole is; entrances and nodes never. Stations grouped by DHID prefix (§3.4) are in scope if any of their poles is. Flagged by the importer after COPY (`stops.in_scope`). Numbers: §7 #12.

## 5. API (contract summary — `api/openapi.yaml` is authoritative)

### 5.1 Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/health` | status + freshness |
| GET | `/v1/layers` | available layers + metadata |
| GET | `/v1/nearby?lat&lng&radius=500&layers=transit` | features around a point, grouped by layer |
| GET | `/v1/stations/{stationId}` | one transit station with all poles and more departures (sheet) |
| POST | `/internal/import` | dev-only: start a GTFS import (`LIVESTOP_DEV=true`, else 404); 202 at once, 409 while one runs |

Rules: `radius` 100–1000 (default 500). `layers` comma-separated, default `transit`. Unknown layer → 400. All times ISO-8601 with offset, Europe/Berlin.

### 5.2 `/v1/nearby` response

```json
{
  "meta": {
    "generatedAt": "2026-11-05T14:30:12+01:00",
    "cell": "u33dc9f",
    "attribution": ["Data: VBB (CC BY 4.0)"],
    "layers": {
      "transit": { "version": 18342, "realtimeAgeSeconds": 11, "status": "ok" }
    }
  },
  "layers": {
    "transit": {
      "layer": "transit",
      "features": [
        {
          "id": "de:11000:900009104",
          "kind": "station",
          "name": "U Wedding",
          "location": { "lat": 52.5427, "lng": 13.3660 },
          "distanceMeters": 140,
          "modes": ["ubahn", "bus"],
          "poles": [
            {
              "id": "de:11000:900009104::1",
              "location": { "lat": 52.5426, "lng": 13.3658 },
              "platform": "1",
              "directions": [
                {
                  "line": "U6",
                  "mode": "ubahn",
                  "color": "#8C6DAB",
                  "headsign": "Alt-Mariendorf",
                  "isTerminal": false,
                  "isRing": false,
                  "departures": [
                    {
                      "tripId": "123456",
                      "serviceDate": "2026-11-05",
                      "scheduled": "2026-11-05T14:32:00+01:00",
                      "predicted": "2026-11-05T14:33:00+01:00",
                      "delaySeconds": 60,
                      "source": "realtime",
                      "cancelled": false
                    }
                  ]
                }
              ]
            }
          ]
        }
      ]
    }
  }
}
```

- `predicted` and `delaySeconds` are null when `source = "scheduled"`.
- Max 3 departures per direction in `/nearby`; max 10 in `/stations/{id}`. Departures start at the snapshot's time (`GeneratedAt`), not the request time, so a response depends only on the snapshot version; the app drops the ones that left since.
- Which directions a pole lists: §3.2 "API directions of a pole".
- `color`: the feed's `route_color`, else VBB's official line colour list (embedded, `backend/internal/transit/data/`, matched by agency, line and mode; §7 #25), else null.
- `modes` (station) and directions are ordered by mode: regional, sbahn, ubahn, tram, bus, ferry, other; directions then by line (natural order: M4 < M10) and headsign.
- Station names and headsigns drop VBB's locality suffix " (Berlin)" (§7 #3); other localities stay ("Rathaus (Potsdam)").
- `/v1/nearby` lists only the poles inside the circle, nearest first; `/v1/stations/{id}` all served poles of the station in scope, ordered by their most important mode (rail platforms first), platform, id.
- Station and pole IDs above are real VBB formats (§7 #3, #4); the `tripId` value is illustrative (real ones are numeric, e.g. `303041996`).

### 5.3 Errors

`{ "error": { "code": "invalid_argument", "message": "radius must be between 100 and 1000" } }` with matching HTTP status.

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_argument` | bad or missing parameters (all problems in one message, e.g. `lat must be between -90 and 90; radius must be between 100 and 1000`), unknown or repeated layer |
| 404 | `not_found` | unknown path; station with no served pole in scope |
| 409 | `conflict` | `/internal/import` while an import runs |
| 500 | `internal` | unexpected error (details only in the log) |
| 503 | `unavailable` | station sheet before the first departures snapshot |

`/v1/nearby` does not fail when a layer has no data: 200, the layer is `down` in `meta.layers` and missing from `layers` (task 2.5).

### 5.4 Caching headers

- `ETag` on every `/v1/nearby` and `/v1/stations/*` response; `304` on match.
- `Cache-Control: no-cache` (clients must revalidate; ETag makes it cheap).

## 6. Mode mapping (`route_type` → `mode`)

VBB uses **extended route types**. Values present in the feed of 2026-08-21 (§7 #5) are marked ✓; the others are fallbacks for future feeds.

| route_type | mode | UI | in feed |
|---|---|---|---|
| 109 | `sbahn` | S-Bahn | ✓ |
| 400 | `ubahn` | U-Bahn | ✓ |
| 900 | `tram` | Tram | ✓ |
| 700, 3 | `bus` | Bus (also rail replacement buses named S…/U…, §7 #6) | ✓ ✓ |
| 1000, 4 | `ferry` | Ferry | ✓ – |
| 100, 106, 2 | `regional` | RE/RB | ✓ ✓ – |
| 0 | `tram` | basic type fallback | – |
| 1 | `ubahn` | basic type fallback | – |
| anything else | `other` | log once per value | – |

## 7. Verified facts (fill during Phase 1+)

> Every entry: fact, how it was verified (query/file), date. Remove the matching **(verify)** marks above.

Evidence tool: `backend/internal/tools/gtfsexplore` (task 1.1, not shipped). Unless noted, numbers are from the **current** feed `https://www.vbb.de/gtfs` downloaded 2026-11-08 (files dated 2026-11-05, sha256 `d9625924…c4df7`), see #15: "static" = `go run ./internal/tools/gtfsexplore static -zip …`, "stops" = `… stops -id/-name`, "rt" = `… rt` on 5 live GTFS-RT samples of 2026-11-08 22:59–23:00 UTC (Sunday → Monday 00:00 Berlin).

| # | Fact | Evidence | Date |
|---|---|---|---|
| 1 | **Static feed shape.** Zip 84,505,287 bytes (~650 MB unzipped). Files: agency (32), calendar, calendar_dates, frequencies (header only, no frequency-based trips), levels, pathways, routes (1,275), shapes (195 MB), stop_times (411 MB, 5,836,294 rows), stops (42,289), transfers, trips (263,715). **No `feed_info.txt`.** | static | 2026-11-08 |
| 2 | **Validity / republishing (verify #9).** All 3,200 `calendar` rows run 2026-11-05 … 2027-12-11; `calendar_dates` same range (101,963 added, 63,767 removed dates). VBB's open-data page states "Aktualisierung: 2x wöchentlich" for this file. The daily check stays; the real cadence will show in `meta.feed_imports`. | static, open-data page | 2026-11-09 |
| 3 | **stop_id formats (verify #1).** Stations (`location_type=1`, 880): `de:<AGS>:<9 digits>`, e.g. `de:11000:900009104` "S+U Wedding (Berlin)". Poles (`0`, 28,105): `de:<AGS>:<station>::<n>` (26,416, e.g. `de:11000:900009104::3`), `de:<AGS>:<station>:<area>:<platform>` (1,133, rail, e.g. `de:11000:900009104:1:50`), bare `de:<AGS>:<station>` (179), numeric non-DHID (352, outside Berlin, e.g. `000000104801`), a few others. Entrances (`2`, 6,494) and generic nodes (`3`, 6,810) have numeric ids (e.g. `000300158001`) and exist for pathways. `stop_times` references only `location_type=0`. Names carry a locality suffix: "(Berlin)". | static, stops | 2026-11-08 |
| 4 | **Pole → station (verify #1).** Only 2,135 poles have `parent_station` (2,099 of them = their DHID prefix). Bus/tram poles of the same station usually have **no parent**: at S+U Wedding the U-Bahn (`::1`, `::2`) and S-Bahn (`:1:50`, `:1:51`) poles have parent `de:11000:900009104`, the bus poles `::3`–`::8` have none. 22,441 poles have a DHID prefix that is not in `stops.txt` at all. ⇒ station = `parent_station`, else DHID prefix, else the pole (13,089 distinct prefixes). A prefix "station" has no row of its own: name and location must be derived from its poles; 53 prefixes have poles with different names (e.g. `de:11000:900003155`: "Alt-Moabit/Rathaus Tiergarten (Berlin)" / "Rathaus Tiergarten (Berlin)"). "S Wedding/Martha-Ndumbe-Platz" (`de:11000:900008103`) is a separate station 150 m away. | static, stops | 2026-11-08 |
| 5 | **route_type values (verify #4).** 700 bus (1,061 routes / 190,077 trips), 3 bus (33 / 497), 900 tram (48 / 31,805), 109 S-Bahn (53 / 22,027), 400 U-Bahn (9 / 11,708), 100 regional (49 / 5,514), 106 regional (14 / 1,358), 1000 ferry (8 / 729). No 0, 1, 2, 4. `route_id` format `<n>_<route_type>`, e.g. `17521_400`. `route_type=109` also contains `RB31` and `RE10`. Tram short names include non-Berlin lines `1`–`6` (Potsdam, Cottbus, …) besides `M1`…`M17`, `12`…`99`. | static, routes.txt | 2026-11-08 |
| 6 | **Rail replacement buses.** 25 `route_type=700` routes carry rail line names (S1 ×2, S2, S3, S3A, S3B, S41, S42, S46, S46A, S46X, S47, S5, S7, S75, S7A, S7B, S8, S9, S9A, S9X, U2, U5, U6). ⇒ mode comes from `route_type`, never from the line name; a "U6" direction can be a bus. | routes.txt | 2026-11-08 |
| 7 | **route_color.** Set for U-Bahn 100 %, S-Bahn 87 %, regional 71–76 %, tram 37.5 %, bus 6 %, ferry 0 %. ⇒ API `color` is often null; the app needs per-mode default colors. VBB publishes official colors for regional, S- and U-Bahn lines separately (`linienfarben.zip` on the open-data page, CSV, April 2026), usable as override; nothing official for tram/bus. | static, open-data page | 2026-11-09 |
| 8 | **direction_id (verify #6).** Set on 100 % of trips (0: 137,504, 1: 126,211). `trip_headsign` empty on only 32 trips (filled with the last stop's name in `pole_directions`). `trip_id` numeric (e.g. `310952817`), `service_id` small integers as text (e.g. `1010`). | static | 2026-11-08 |
| 9 | **platform_code (verify #7).** 6.5 % of all poles. By mode of the poles' trips: U-Bahn 99.0 % (396 poles), S-Bahn 54.3 % (451), regional 66–67 %, tram 10.4 %, bus 1.8 %, ferry 0 %. ⇒ `Pole.platform` is nullable; the UI needs a fallback label (e.g. direction headsign). | static | 2026-11-08 |
| 10 | **Times past 24:00 (verify #8).** Max `departure_time` `31:17:00` (trip 310876822). 223,320 departures (3.8 %) ≥ 24:00, 30,448 ≥ 27:00, 689 ≥ 30:00; 8,921 trips *start* ≥ 24:00. No unparseable times. ⇒ the schedule window must include trips of the previous service day up to +31:17 h (use a margin, e.g. 32 h). `pickup_type`/`drop_off_type` mostly 0; 5,644 no-pickup and 3,328 no-drop-off stop times. | static | 2026-11-08 |
| 11 | **Ring and loop lines (verify #5).** S41/S42 (`route_type=109`): full-circle trips start and end at S Beusselstr. with headsign **"Ringbahn S 41"** / **"Ringbahn S 42"** (909 / 1,062 trips, all `direction_id=0`); partial trips have normal station headsigns (e.g. "S Westend (Berlin)"). ⇒ `isRing` = S-Bahn line S41/S42, or headsign starting with "Ringbahn". 67 other lines (mostly buses, e.g. 365 "S Baumschulenweg") have loop trips (first station = last station, headsign = that station). ⇒ terminal/origin detection (§3.2) must treat loop trips separately, otherwise a loop's start pole is marked terminal and its departures are hidden. | static (loops section) | 2026-11-08 |
| 12 | **Scope (bbox), task 1.4.** The default bbox `13.088,52.338,13.761,52.675` contains **10,841 poles** in **3,914 stations** (grouped as §3.4; without the DHID rule it would be 9,912 "stations"). Berlin (AGS `11000`): 2,974 stations, 8,903 poles; the rest are Brandenburg suburbs inside the rectangle (e.g. AGS 12069 Potsdam-Mittelmark 140 stations, 12054 Potsdam 94). Poles served per mode: bus 8,198 (700 + 3), tram 893, U-Bahn 396 (all of them), S-Bahn 339, regional 133, ferry 13. **1,184 in-scope poles have no stop_times at all** (not served in this feed) → only served poles get directions/markers. Entrances and nodes (`location_type` 2/3) are never flagged. | LoadRealFeed test on the 2026-11-05 feed | 2026-11-09 |
| 13 | **GTFS-RT HTTP.** `HEAD`/`GET` return `ETag` and `Last-Modified`; `If-None-Match` → `304` with empty body. gzip and zstd are supported (ETag differs per encoding). Live: `Content-Type: application/protobuf; schedule_sha256=d8202ba3` (meaning unknown: it is not the sha256 of either static zip). ~4.9–6.7 MB raw, ~1 MB gzip. `Last-Modified` changes every few seconds; one sample's `header.timestamp` was already 2 min 14 s old at fetch (lag happens). A staging feed exists at `https://staging.gtfsrt.vbb.de/data` for testing new feed features. Source code: https://github.com/OpenDataVBB/gtfs-rt-feed. | record, HEAD, open-data page | 2026-11-09 |
| 14 | **GTFS-RT can be empty and stale.** From ≤ 22:45 to ≥ 22:57 UTC on 2026-11-08 the feed returned a 15-byte FeedMessage: version 2.0, FULL_DATASET, **0 entities**, `header.timestamp` 2026-10-30T19:04:09Z (9 days old). Before and after, a live feed was served. ⇒ freshness and health must use `header.timestamp`, not HTTP success; an empty feed is "no data", not "no delays", and must not wipe realtime state silently. Fixture: `backend/testdata/gtfsrt/vbb-empty-stale.pb`. | record, rt | 2026-11-08 |
| 15 | **Which static feed matches GTFS-RT.** The former default `LIVESTOP_GTFS_URL` (unternehmen.vbb.de `gtfs-mastscharf/GTFS.zip`) is **stale**: Last-Modified 2026-08-21, valid only until 2026-12-12; only 46.9 % of RT `trip_id`s exist in it (almost only S-Bahn). The open-data page (https://unternehmen.vbb.de/digitale-services/datensaetze/) links **`https://unternehmen.vbb.de/gtfs`**; `https://www.vbb.de/gtfs` (and `/vbbgtfs` → 307) serves the same file (same length, `filename="gtfs.zip"`). It is equally pole-precise and **100 %** of RT `trip_id`s and 98.7 % of RT `stop_id`s match it. It sends **no ETag / Last-Modified** → change detection by SHA-256 only (full 84.5 MB download per check). A separate "inkl. Flex" download (`/gtfs-flex`, 31 KB) holds only on-demand services; not used. Default switched to the official link on 2026-11-09. | download, rt with both zips, open-data page | 2026-11-09 |
| 16 | **GTFS-RT content.** FULL_DATASET with **TripUpdates only** (no VehiclePositions, no Alerts), ~4,900 entities. `header.timestamp` 2–18 s old at fetch. TripDescriptor: `trip_id`, `route_id`, `start_date`, `direction_id` always set; **`start_time` never**; `schedule_relationship` SCHEDULED, rarely CANCELED (5 of 24,686). `TripUpdate.timestamp` always set, `TripUpdate.delay` and `vehicle` never. | rt | 2026-11-08 |
| 17 | **RT trip identity (verify #3).** RT uses the static `trip_id` + `start_date` (`YYYYMMDD`) directly; `route_id` equals the static one in 100 %. `start_date` is the service day: at 00:00 Berlin on 2026-11-09, 5,893 of 24,686 updates still had `start_date=20261108` (trips of the previous service day). | rt | 2026-11-08 |
| 18 | **RT stop identity (verify #2).** `stop_id` always set: 98.7 % are static poles (`location_type=0`), never stations; 1.3 % are bare station numbers without DHID (e.g. `900220139` for poles `de:12069:900220139::1`/`::2`) → pole unknown, use `stop_sequence` (set on 98.7 %). Times are absolute: `arrival.time` / `departure.time` set on ~99.8 %, `delay` only on ~24 %. The feed's README explains it: each matched trip is merged with a TripUpdate built from the schedule, so **an event with `time` but no `delay` is schedule-filled, not realtime**. ⇒ a departure counts as `source=realtime` only if its event has `delay` (or a time that differs from the schedule); delay = time − scheduled. STU `schedule_relationship`: SCHEDULED 471,907, SKIPPED 6,015, NO_DATA 331. ~19 STUs per TripUpdate. Arrival delay: min −281 s, p10 −60 s, median 0, p90 +60 s, max +2,520 s. | rt | 2026-11-08 |
| 19 | **RT coverage by mode.** Share of TripUpdates (5 samples, Sunday 23:59 UTC): S-Bahn 61 %, regional 18 %, bus 15 %, tram 3.5 %, U-Bahn 1.7 %. One more sample at 23:27 UTC: 6,866 TripUpdates, S-Bahn 47 %, bus 36 %, regional 13 %, tram 2.6 %, U-Bahn 1.4 %. The provider page states "limited data coverage since 2026-06-04". ⇒ for BVG U-Bahn and tram, scheduled-only departures are the normal case; with "realtime = has `delay`" (#18) the real share is lower still. Measure at daytime on a weekday before Phase 4. | rt, provider page | 2026-11-09 |
| 20 | **GTFS-RT usage terms** (provider page https://production.gtfsrt.vbb.de/): rate limit **7 requests/minute since 2026-11-02** (was 60; "allows fetching every 10 seconds"); send `Accept-Encoding: gzip` or `zstd`; informative `User-Agent` with contact info required, generic ones may be blocked; Atom feed for downtime notices; CORS enabled; CC BY 4.0. | provider page | 2026-11-08 |
| 21 | **stop_headsign.** Set on 40 % of stop_times (2,327,520 of 5,836,294). Of those, 28 % equal the trip headsign or drop its " (Berlin)" suffix ("U Hermannplatz/Urbanstr. (Berlin)" → "U Hermannplatz"); **72 % are a different text**: the intermediate destination shown on the vehicle at that stop (M27 to "Hadlichstr." shows "S+U Pankow") or a real name where the trip headsign is generic (trip "Berlin" → stop "Stralau"). ⇒ group directions by `trip_headsign` (stable), show `display_headsign` (most common stop headsign at the pole). In the derived table, 29,950 of 125,215 directions show a text other than their trip headsign. | awk over stop_times.txt + trips.txt; LoadRealFeed | 2026-11-09 |
| 22 | **Derived tables (task 1.5), current feed.** `service_dates` 257,451 rows (whole validity); `pole_directions` 125,215 (7,066 terminal, 7,933 origin, 1,525 ring); `stations` 13,431 in the whole VBB area, 3,914 in scope, 3,881 of them with at least one direction. Build time on the dev PC: COPY ~10 s, keys/indexes + ANALYZE ~20–60 s, derived tables ~55–70 s; whole import 1.5–2.7 min, peak Go memory 18–22 MB. Wedding subset fixture (`backend/testdata/gtfs/vbb-wedding-800m.zip`, 2.7 MB): 17 stations, 13,394 trips, 343,256 stop_times; it contains every trip of the GTFS-RT fixtures. | LoadRealFeed, TestWeddingSubset | 2026-11-09 |
| 23 | **Nearby query (task 1.8)** `NearbyPoles` (`backend/db/queries/nearby.sql`): served, in-scope poles in a circle, grouped by station, nearest station first. "Wedding 500 m" (52.5427, 13.3660): 23 poles in 7 stations; S+U Wedding is **one** station with its 6 served poles (U `::1`/`::2`, S `:1:50`/`:1:51`, bus `::3`/`::4`; unserved `::5`/`::6`/`::8` left out); S Wedding/Martha-Ndumbe-Platz separate at 293 m. Speed on the full feed (GiST `stops_geom_idx` + `pole_directions_stop_idx`, no seq scan): median **2.8 ms** via pgx (prepared, warm); Alexanderplatz 1000 m (densest, max radius) 119 poles, 11.9 ms; the very first call after start ~90 ms (planning + cold cache). **Same name, two stations:** "U Reinickendorfer Str." is `de:11000:900008104` (bus) and `de:11000:900008102` (U-Bahn + bus), 200 m apart — DHID splits them, so the API will list both; how to present same-name neighbours is a Phase 2/3 UI question. | NearbyPoles on the dev DB (EXPLAIN ANALYZE) and in TestImportRealFeed / TestNearbyPolesWedding | 2026-11-09 |

| 24 | **Schedule window (task 2.2).** Feed of 2026-11-08 (282,294 trips, 6,247,945 stop_times): 17:00 + 92 min → **88,665** boardable departures at in-scope poles; a 1-minute slice ~900; 00:15 + 92 min → 23,170. In the Wedding subset **all** 1,629 departures between 00:15 and 01:45 on a Tuesday belong to Monday's service day (coded 24:15–25:45): VBB's night network lives on the previous service day, so the window must always ask yesterday too. Loader = plain SQL definition (NOT EXISTS later stop, service day noon − 12 h in SQL) on the Wedding subset, both windows. Speed: dev DB 92-min window ~0.5 s, night window ~0.15 s, 1-minute slice ~70 ms; in the fresh test container right after the import 1.3 s / 1.0 s / 170 ms. | TestLoadWindowWedding, TestImportRealFeed, EXPLAIN ANALYZE on the dev DB | 2026-11-09 |
| 25 | **VBB line colour list (`linienfarben.zip`, task 2.4).** 2,540-byte zip with `20260828-linienfarben.csv` (ISO-8859-1, CRLF, `;`), 155 rows: regional, S-Bahn, U-Bahn, plus city tram/bus lines of Brandenburg an der Havel, Cottbus, Frankfurt (Oder) and Potsdam; 3 generic rows without agency (`Tram` #E2001A, `Bus` #A5027D, `Fähre` #009BD5 — candidates for the app's per-mode defaults, task 7.1). Columns `Linie`, `agency_id`, `route_Type`, R/G/B, `background_Hex` (empty on 72 rows), text R/G/B. `agency_id` uses the GTFS ids (1 S-Bahn Berlin, 796 BVG, 108 DB Regio, …). Against the 2026-11-05 feed: 130 of 152 line rows match a route by (agency, short name, route_type); 9 regional rows say 100 where the feed says 106 (same mode), RB34/RB66 are buses (rail replacement) in the feed, others (S45, Brandenburg buses B–W under another agency) have no route. **Where a route has `route_color`, it equals the list (0 of 155 differ).** ⇒ key = (agency, line, mode). In-scope directions with a colour: S-Bahn 97.5 % → **100 %**, regional 91.4 % → 97.7 %, U-Bahn 100 %, tram 2.9 %, bus 2.6 %, ferry 0 % (unchanged). | psql on the dev DB with the CSV in a temp table | 2026-11-09 |
| 26 | **Direction variants per pole.** `pole_directions` has a row per (route, direction_id, trip headsign), so one platform carries many variants: S+U Wedding S-Bahn platform 1 lists S41 "Ringbahn S 41" from 10+ trip headsigns, plus S41 to S Greifswalder Str./S Ostkreuz/S Treptower Park; U6 platform 2 has U Alt-Tegel, U Kurt-Schumacher-Platz and U Seestr.; bus poles have night variants (N6 to 4 destinations). Wedding subset, 500 m around U Wedding, Tue 2026-11-10 17:00: 174 rows at 23 poles, 121 distinct labels, **51** directions after the §3.2 rules (99 departures). That afternoon the U6 north of Wedding ran only to U Kurt-Schumacher-Platz in this feed (Alt-Tegel variant without departures). Full feed: Wedding 500 m 52 directions (26 KB JSON), Alexanderplatz 1000 m 27 stations / 297 directions (135 KB JSON uncompressed); transit `Nearby` median 4.5 ms / 12.1 ms. | TestProviderWeddingGolden (testdata/*.golden.json), TestImportRealFeed | 2026-11-09 |
| 27 | **Spot check vs. VBB departure board (task 2.6).** 5 stations, 69 directions (S, U, tram, bus, regional, ferry), Monday 2026-11-09 13:06–14:38: **every scheduled time equals fahrinfo.vbb.de**. Not times: (a) lines suspended for weeks keep a direction with an empty list (§3.2 fallback): RE2/RE7/RB14/RB23 stop at S+U Alexanderplatz only from 2026-12-13 in this feed, trams M1/12 at U Eberswalder Str. replaced by buses; (b) some `trip_headsign`s are less precise than VBB's own board (RE1 "Hauptbahnhof" = Magdeburg Hbf, loop bus 114 "S Wannsee Bhf" vs "Heckeshorn"); (c) VBB's board merges nearby stops (Alexanderplatz/Gontardstr., S Wedding/Martha-Ndumbe-Platz) that DHID keeps apart. | browser: fahrinfo.vbb.de webapp vs `/v1/stations/{id}` on the dev DB | 2026-11-09 |

All Phase 1 **(verify)** items are answered. Fixtures: `backend/testdata/gtfsrt/` and `backend/testdata/gtfs/` (READMEs there).

## 8. Go in-memory types (reference)

`transit/departures` (tasks 2.2, 2.3). The loader returns a `Window`; the builder publishes a `Snapshot`.

```go
type Departure struct {
    TripID       string
    ServiceDate  ServiceDay // {Year, Month, Day}; with TripID the trip instance realtime refers to
    StopID       string
    StopSequence int32
    RouteID      string
    DirectionID  int16      // NoDirection (-1) if the feed has none
    Headsign     string     // direction key = pole_directions.headsign
    StopHeadsign string     // what the vehicle shows at this stop; "" if none
    Scheduled    time.Time  // Europe/Berlin
}

// Immutable once published (ADR-0005); read without locks.
type Snapshot struct {
    Version     uint64    // +1 per snapshot, starts at the process start time in Unix ms
    GeneratedAt time.Time
    ScheduleID  int64     // OID of the live gtfs schema the departures come from
    From, To    time.Time // window covered, half-open
    // unexported: one array grouped by pole; Departures(stopID) returns a
    // pole's departures sorted by time, Len(), Stops()
}
```

Planned:
- Line, mode and colour are per route (`route_id`). Task 2.4 decides where they are looked up.
- Phase 4 adds the realtime fields: `Predicted *time.Time`, `DelaySeconds *int32`, `Source` (scheduled | realtime), `Cancelled`, and `Snapshot.RealtimeFeedTime` (`header.timestamp` of the last applied feed). Then the order inside a pole is by effective time (predicted, else scheduled).

## 9. Mobile core types (reference)

Generated API types are the source; `src/core` adds derived values only:

```ts
type EffectiveDeparture = Departure & {
  effectiveAt: number;          // epoch ms: predicted ?? scheduled
  minutesLeft: number;          // computed from now
  label: string;                // "now" | "3 min" | "14:52"
};
```

Display rules: `< 1 min` → "now"; `≤ 59 min` → "N min"; `≥ 60 min` → clock time `HH:mm`. Cancelled → struck through, not counted as "next".

Details (`src/core/time.ts`, `src/core/departures.ts`, task 3.1):
- Minutes are rounded down: 1 min 59 s left → "1 min".
- The clock time is taken as written in the timestamp (`14:52` from `…T14:52:00+01:00`), i.e. Berlin time whatever the device's timezone; no Intl timezone data needed.
- A departure is gone one minute after its effective time (GTFS times are whole minutes: a train due 14:32 leaves during 14:32 and shows "now" until 14:33).
- Timestamps without seconds or offset are rejected (would be read as device time); such a departure is dropped.
- Upcoming departures are ordered by effective time (a late train can fall behind the next one); equal times keep the API order.

## 10. Future layer: alerts (Phase 8, design sketch only)

```
Alert {
  id, sources[]            // e.g. ["vbb-verkehrsmeldungen", "hafas"]
  severity                 // info | warning | severe
  cause, effect            // GTFS-RT ServiceAlert vocab where possible
  activePeriods[]          // {start, end?}
  informed[]               // {routeId?, line?, stopId?}
  header, description      // localized {de, en?}
  updatedAt
}
```
De-duplication: same informed entity + overlapping period + similar header → merge, union of `sources`.
