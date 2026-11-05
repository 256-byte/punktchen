# Data Model

> Source of truth for data structures. If code and this document disagree, fix one of them in the same change.
> Anything marked **(verify)** is an assumption that must be checked against real data in Phase 1 and moved to §7.

## 1. Glossary

| Term | Meaning |
|---|---|
| **GTFS** | General Transit Feed Specification. Zip of CSV files describing the schedule. |
| **GTFS-RT** | GTFS Realtime. Protobuf feed with delays/cancellations (TripUpdates), positions, alerts. |
| **VDV** | German transport association standards (VDV 452/453/454). Upstream source of VBB realtime; we never consume VDV directly. |
| **Station** | Parent stop (`location_type = 1`), e.g. "U Wedding". |
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
| `station_id` | text | parent station or the pole itself if no parent |
| `route_id` | text | |
| `line` | text | `route_short_name`, e.g. `U6`, `S1`, `M27` |
| `mode` | text | mapped from `route_type`, §6 |
| `direction_id` | smallint | may be null in data **(verify)** |
| `headsign` | text | e.g. `Alt-Tegel`, `Wannsee` |
| `is_terminal` | bool | pole is the last stop for ≥ 95% of trips of this (route, direction, headsign) |
| `is_origin` | bool | pole is the first stop of those trips |
| `trip_count` | int | trips in the next 7 days, for ordering |

Terminal rule for the UI: at a terminal pole, arrivals that end there are **not** shown as departures; only the opposite direction is shown.

### 3.3 `meta` schema (not swapped)

- `meta.feed_imports(id, source, url, etag, last_modified, sha256, started_at, finished_at, status, row_counts jsonb, error)`
- `meta.schema_migrations` (golang-migrate)

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
- IDs above are illustrative **(verify)** — use real formats found in Phase 1.

### 5.3 Errors

`{ "error": { "code": "invalid_argument", "message": "radius must be between 100 and 1000" } }` with matching HTTP status.

### 5.4 Caching headers

- `ETag` on every `/v1/nearby` and `/v1/stations/*` response; `304` on match.
- `Cache-Control: no-cache` (clients must revalidate; ETag makes it cheap).

## 6. Mode mapping (`route_type` → `mode`)

VBB uses **extended route types** **(verify exact values in Phase 1)**:

| route_type | mode | UI |
|---|---|---|
| 109 | `sbahn` | S-Bahn |
| 400 | `ubahn` | U-Bahn |
| 900 | `tram` | Tram |
| 700, 3 | `bus` | Bus |
| 1000, 4 | `ferry` | Ferry |
| 100, 106, 2 | `regional` | RE/RB |
| 0 | `tram` | basic type fallback |
| 1 | `ubahn` | basic type fallback |
| anything else | `other` | log once per value |

## 7. Verified facts (fill during Phase 1+)

> Every entry: fact, how it was verified (query/file), date. Remove the matching **(verify)** marks above.

| # | Fact | Evidence | Date |
|---|---|---|---|
| — | — | — | — |

Things to verify in Phase 1:
1. `stop_id` format in static GTFS (e.g. `de:11000:900009104::1`?) and how stations/poles relate.
2. `stop_id` format used in GTFS-RT `stop_time_update` and whether it matches static IDs 1:1.
3. Whether GTFS-RT uses `trip_id` + `start_date` from the static feed directly.
4. Actual `route_type` values present.
5. Headsign values of ring lines S41/S42 (and any other ring/loop lines) → how to set `isRing`.
6. Whether `direction_id` is populated.
7. Presence of `platform_code` for U-Bahn/S-Bahn poles.
8. Max `departure_time` value (how far past 24:00 times go).
9. Feed validity range (`calendar.end_date`) and how often VBB republishes.

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
