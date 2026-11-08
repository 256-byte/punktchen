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
- `arrival_time` / `departure_time` stored as **integer seconds since service-day start** (`departure_secs`), because values like `25:10:00` exist. Keep the raw text column too.
- `stops.geom geography(Point, 4326)` + GiST index.
- Extra column `stops.in_scope boolean` (scope filter, §4).
- Indexes: `stop_times(stop_id, departure_secs)`, `stop_times(trip_id, stop_sequence)`, `trips(service_id)`, `stops(parent_station)`, GiST on `stops.geom`.

## 3. Derived tables (built after each import, schema `gtfs`)

### 3.1 `service_dates`
Expanded calendar for `[today - 1, today + 7]`.

| column | type |
|---|---|
| `service_date` | date |
| `service_id` | text |

Built from `calendar` (weekday + range) plus `calendar_dates` (1 = add, 2 = remove). Rebuilt daily even if the feed did not change.

### 3.2 `pole_directions`
What leaves from each pole. Used for markers and for showing directions with no departure in the window.

| column | type | notes |
|---|---|---|
| `stop_id` | text | pole |
| `station_id` | text | `parent_station`, else DHID prefix `de:<AGS>:<station>`, else the pole itself (§7 #4) |
| `route_id` | text | |
| `line` | text | `route_short_name`, e.g. `U6`, `S1`, `M27` |
| `mode` | text | mapped from `route_type`, §6 |
| `direction_id` | smallint | always 0/1 in VBB data (§7 #8); nullable column anyway (optional in GTFS) |
| `headsign` | text | e.g. `Alt-Tegel`, `Wannsee` |
| `is_terminal` | bool | pole is the last stop for ≥ 95% of trips of this (route, direction, headsign) |
| `is_origin` | bool | pole is the first stop of those trips |
| `trip_count` | int | trips in the next 7 days, for ordering |

Terminal rule for the UI: at a terminal pole, arrivals that end there are **not** shown as departures; only the opposite direction is shown.

### 3.3 `meta` schema (not swapped)

- `meta.feed_imports(id, source, url, etag, last_modified, sha256, started_at, finished_at, status, row_counts jsonb, error)` — migration `0001_meta`
  - `status`: `running` | `succeeded` | `failed` | `skipped` (unchanged upstream).
  - `finished_at` is NULL exactly while `running`; `error` only on `failed`; `sha256` is lowercase hex or NULL (nothing downloaded).
  - `etag` / `last_modified` are stored as raw header text, to send back unchanged in conditional GETs.
- `meta.schema_migrations` (golang-migrate, version + dirty flag). Migrations are embedded in the binary (`backend/db/migrations`) and run on every startup.

## 4. Scope

- Import the **full VBB feed**; restrict by `stops.in_scope`.
- Default scope: Berlin bounding box `13.088,52.338,13.761,52.675` (minLon,minLat,maxLon,maxLat), config `LIVESTOP_SCOPE_BBOX`. Can later become a polygon.
- A pole is in scope if its coordinates are inside; a station is in scope if any child pole is.

## 5. API (contract summary — `api/openapi.yaml` is authoritative)

### 5.1 Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/health` | status + freshness |
| GET | `/v1/layers` | available layers + metadata |
| GET | `/v1/nearby?lat&lng&radius=500&layers=transit` | features around a point, grouped by layer |
| GET | `/v1/stations/{stationId}` | one transit station with all poles and more departures (sheet) |
| POST | `/internal/import` | dev-only manual import trigger (`LIVESTOP_DEV=true`) |

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
- Max 3 departures per direction in `/nearby`; max 10 in `/stations/{id}`.
- Station and pole IDs above are real VBB formats (§7 #3, #4); the `tripId` value is illustrative (real ones are numeric, e.g. `303041996`).

### 5.3 Errors

`{ "error": { "code": "invalid_argument", "message": "radius must be between 100 and 1000" } }` with matching HTTP status.

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
| 2 | **Validity / republishing (verify #9).** All 3,200 `calendar` rows run 2026-11-05 … 2027-12-11; `calendar_dates` same range (101,963 added, 63,767 removed dates). VBB publishes a new file irregularly (previous one: 2026-08-21, see #15), not daily; the daily check stays, the real cadence will show in `meta.feed_imports`. | static | 2026-11-08 |
| 3 | **stop_id formats (verify #1).** Stations (`location_type=1`, 880): `de:<AGS>:<9 digits>`, e.g. `de:11000:900009104` "S+U Wedding (Berlin)". Poles (`0`, 28,105): `de:<AGS>:<station>::<n>` (26,416, e.g. `de:11000:900009104::3`), `de:<AGS>:<station>:<area>:<platform>` (1,133, rail, e.g. `de:11000:900009104:1:50`), bare `de:<AGS>:<station>` (179), numeric non-DHID (352, outside Berlin, e.g. `000000104801`), a few others. Entrances (`2`, 6,494) and generic nodes (`3`, 6,810) have numeric ids (e.g. `000300158001`) and exist for pathways. `stop_times` references only `location_type=0`. Names carry a locality suffix: "(Berlin)". | static, stops | 2026-11-08 |
| 4 | **Pole → station (verify #1).** Only 2,135 poles have `parent_station` (2,099 of them = their DHID prefix). Bus/tram poles of the same station usually have **no parent**: at S+U Wedding the U-Bahn (`::1`, `::2`) and S-Bahn (`:1:50`, `:1:51`) poles have parent `de:11000:900009104`, the bus poles `::3`–`::8` have none. 22,441 poles have a DHID prefix that is not in `stops.txt` at all. ⇒ station = `parent_station`, else DHID prefix, else the pole (13,089 distinct prefixes). A prefix "station" has no row of its own: name and location must be derived from its poles; 53 prefixes have poles with different names (e.g. `de:11000:900003155`: "Alt-Moabit/Rathaus Tiergarten (Berlin)" / "Rathaus Tiergarten (Berlin)"). "S Wedding/Martha-Ndumbe-Platz" (`de:11000:900008103`) is a separate station 150 m away. | static, stops | 2026-11-08 |
| 5 | **route_type values (verify #4).** 700 bus (1,061 routes / 190,077 trips), 3 bus (33 / 497), 900 tram (48 / 31,805), 109 S-Bahn (53 / 22,027), 400 U-Bahn (9 / 11,708), 100 regional (49 / 5,514), 106 regional (14 / 1,358), 1000 ferry (8 / 729). No 0, 1, 2, 4. `route_id` format `<n>_<route_type>`, e.g. `17521_400`. `route_type=109` also contains `RB31` and `RE10`. Tram short names include non-Berlin lines `1`–`6` (Potsdam, Cottbus, …) besides `M1`…`M17`, `12`…`99`. | static, routes.txt | 2026-11-08 |
| 6 | **Rail replacement buses.** 25 `route_type=700` routes carry rail line names (S1 ×2, S2, S3, S3A, S3B, S41, S42, S46, S46A, S46X, S47, S5, S7, S75, S7A, S7B, S8, S9, S9A, S9X, U2, U5, U6). ⇒ mode comes from `route_type`, never from the line name; a "U6" direction can be a bus. | routes.txt | 2026-11-08 |
| 7 | **route_color.** Set for U-Bahn 100 %, S-Bahn 87 %, regional 71–76 %, tram 37.5 %, bus 6 %, ferry 0 %. ⇒ API `color` is often null; the app needs per-mode default colors. | static | 2026-11-08 |
| 8 | **direction_id (verify #6).** Set on 100 % of trips (0: 137,504, 1: 126,211). `trip_headsign` never empty. `trip_id` numeric (e.g. `310952817`), `service_id` small integers as text (e.g. `1010`). | static | 2026-11-08 |
| 9 | **platform_code (verify #7).** 6.5 % of all poles. By mode of the poles' trips: U-Bahn 99.0 % (396 poles), S-Bahn 54.3 % (451), regional 66–67 %, tram 10.4 %, bus 1.8 %, ferry 0 %. ⇒ `Pole.platform` is nullable; the UI needs a fallback label (e.g. direction headsign). | static | 2026-11-08 |
| 10 | **Times past 24:00 (verify #8).** Max `departure_time` `31:17:00` (trip 310876822). 223,320 departures (3.8 %) ≥ 24:00, 30,448 ≥ 27:00, 689 ≥ 30:00; 8,921 trips *start* ≥ 24:00. No unparseable times. ⇒ the schedule window must include trips of the previous service day up to +31:17 h (use a margin, e.g. 32 h). `pickup_type`/`drop_off_type` mostly 0; 5,644 no-pickup and 3,328 no-drop-off stop times. | static | 2026-11-08 |
| 11 | **Ring and loop lines (verify #5).** S41/S42 (`route_type=109`): full-circle trips start and end at S Beusselstr. with headsign **"Ringbahn S 41"** / **"Ringbahn S 42"** (909 / 1,062 trips, all `direction_id=0`); partial trips have normal station headsigns (e.g. "S Westend (Berlin)"). ⇒ `isRing` = S-Bahn line S41/S42, or headsign starting with "Ringbahn". 67 other lines (mostly buses, e.g. 365 "S Baumschulenweg") have loop trips (first station = last station, headsign = that station). ⇒ terminal/origin detection (§3.2) must treat loop trips separately, otherwise a loop's start pole is marked terminal and its departures are hidden. | static (loops section) | 2026-11-08 |
| 12 | **Scope (bbox).** Default Berlin bbox contains 10,841 poles. Station count depends on the grouping of #4; computed in task 1.4. | static | 2026-11-08 |
| 13 | **GTFS-RT HTTP.** `HEAD`/`GET` return `ETag` and `Last-Modified`; `If-None-Match` → `304` with empty body. gzip and zstd are supported (ETag differs per encoding). Live: `Content-Type: application/protobuf; schedule_sha256=d8202ba3` (meaning unknown: it is not the sha256 of either static zip). ~4.9 MB raw, ~1 MB gzip. `Last-Modified` changes every few seconds. | record, HEAD | 2026-11-08 |
| 14 | **GTFS-RT can be empty and stale.** From ≤ 22:45 to ≥ 22:57 UTC on 2026-11-08 the feed returned a 15-byte FeedMessage: version 2.0, FULL_DATASET, **0 entities**, `header.timestamp` 2026-10-30T19:04:09Z (9 days old). Before and after, a live feed was served. ⇒ freshness and health must use `header.timestamp`, not HTTP success; an empty feed is "no data", not "no delays", and must not wipe realtime state silently. Fixture: `backend/testdata/gtfsrt/vbb-empty-stale.pb`. | record, rt | 2026-11-08 |
| 15 | **Which static feed matches GTFS-RT.** The former default `LIVESTOP_GTFS_URL` (unternehmen.vbb.de `gtfs-mastscharf/GTFS.zip`) is **stale**: Last-Modified 2026-08-21, valid only until 2026-12-12; only 46.9 % of RT `trip_id`s exist in it (almost only S-Bahn). `https://www.vbb.de/gtfs` (also via `/vbbgtfs` → 307) is the current, equally pole-precise feed: **100 %** of RT `trip_id`s and 98.7 % of RT `stop_id`s match it. It sends **no ETag / Last-Modified** → change detection by SHA-256 only (full 84.5 MB download per check). Default switched to it on 2026-11-08. | download, rt with both zips | 2026-11-08 |
| 16 | **GTFS-RT content.** FULL_DATASET with **TripUpdates only** (no VehiclePositions, no Alerts), ~4,900 entities. `header.timestamp` 2–18 s old at fetch. TripDescriptor: `trip_id`, `route_id`, `start_date`, `direction_id` always set; **`start_time` never**; `schedule_relationship` SCHEDULED, rarely CANCELED (5 of 24,686). `TripUpdate.timestamp` always set, `TripUpdate.delay` and `vehicle` never. | rt | 2026-11-08 |
| 17 | **RT trip identity (verify #3).** RT uses the static `trip_id` + `start_date` (`YYYYMMDD`) directly; `route_id` equals the static one in 100 %. `start_date` is the service day: at 00:00 Berlin on 2026-11-09, 5,893 of 24,686 updates still had `start_date=20261108` (trips of the previous service day). | rt | 2026-11-08 |
| 18 | **RT stop identity (verify #2).** `stop_id` always set: 98.7 % are static poles (`location_type=0`), never stations; 1.3 % are bare station numbers without DHID (e.g. `900220139` for poles `de:12069:900220139::1`/`::2`) → pole unknown, use `stop_sequence` (set on 98.7 %). Times are absolute: `arrival.time` / `departure.time` set on ~99.8 %, `delay` only on ~24 % ⇒ compute delay = time − scheduled ourselves. STU `schedule_relationship`: SCHEDULED 471,907, SKIPPED 6,015, NO_DATA 331. ~19 STUs per TripUpdate. Arrival delay: min −281 s, p10 −60 s, median 0, p90 +60 s, max +2,520 s. | rt | 2026-11-08 |
| 19 | **RT coverage by mode.** Share of TripUpdates (5 samples, Sunday midnight): S-Bahn 61 %, regional 18 %, bus 15 %, tram 3.5 %, U-Bahn 1.7 %. The provider page states "limited data coverage since 2026-06-04". ⇒ for BVG U-Bahn, tram and bus, scheduled-only departures are the normal case. Repeat at daytime before Phase 4. | rt, provider page | 2026-11-08 |
| 20 | **GTFS-RT usage terms** (provider page https://production.gtfsrt.vbb.de/): rate limit **7 requests/minute since 2026-11-02** (was 60; "allows fetching every 10 seconds"); send `Accept-Encoding: gzip` or `zstd`; informative `User-Agent` with contact info required, generic ones may be blocked; Atom feed for downtime notices; CORS enabled; CC BY 4.0. | provider page | 2026-11-08 |

All Phase 1 **(verify)** items are answered. Fixtures: `backend/testdata/gtfsrt/` (README there).

## 8. Go in-memory types (reference)

```go
type Departure struct {
    TripID       string
    ServiceDate  civil.Date      // or time.Time at midnight Berlin
    StopID       string
    RouteID      string
    Line         string
    Mode         Mode
    Headsign     string
    DirectionID  *int8
    Scheduled    time.Time
    Predicted    *time.Time
    DelaySeconds *int32
    Source       Source          // SourceScheduled | SourceRealtime
    Cancelled    bool
}

type Snapshot struct {
    Version          uint64
    GeneratedAt      time.Time
    RealtimeFeedTime time.Time      // header.timestamp of last applied RT feed
    ByStop           map[string][]Departure // sorted by effective time
}
```

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
