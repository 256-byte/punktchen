# 0010 — Amenity layers: public toilets and drinking fountains

- **Status:** proposed
- **Date:** 2026-11-18

## Context
The user wants the map to show more than departures: public toilets and drinking fountains near the user (Phase 9). Both are official Berlin open data, served by the Geoportal Berlin (`gdi.berlin.de`) as OGC WFS with GeoJSON output, licence Datenlizenz Deutschland – Zero 2.0 (DATA_MODEL §7 #34):

- `toiletten:toiletten`: 525 points (290 KB), with address, operator, opening hours (free text), fee, payment, barrier-free, changing table.
- `trinkwasserbrunnen:trinkwasserbrunnen`: 242 points (116 KB), with location, type, restrictions (out of service), season (May–October).

Constraints: the app never talks to third parties (CLAUDE.md rule 6); upstream sends no `ETag` / `Last-Modified`; the data changes rarely; both sets are sparse (inside the S-Bahn ring 21 % of places have no toilet and 47 % no fountain within the 590 m that `/v1/nearby` searches).

ADR-0008 already has the generic `layer.Provider`, the composite `/v1/nearby?layers=…` and the mobile layer registry. This ADR decides what the first non-transit layers look like.

## Decision
1. **Two layers, one implementation.** Layer ids `toilets` and `fountains`, each its own `layer.Provider` and its own typed payload in `api/openapi.yaml` (`ToiletsLayer`, `FountainsLayer`, discriminated by `layer` as ADR-0008 says). Both are built by one package, `internal/amenities`: a dataset = WFS URL + type name + a mapping from WFS properties to our columns. The app can switch each layer on and off by itself.
2. **Storage in PostGIS, schema `amenities`.** One table per dataset with typed columns and a `geography(Point)` column (GiST index). An import replaces a dataset's rows in **one transaction** (readers see the old or the new rows, never a mix); no staging-schema swap (ADR-0004) is needed for a few hundred rows. Each run is logged in the existing `meta.feed_imports` (`source` = `berlin-toilets` / `berlin-fountains`).
3. **Import:** at startup when a dataset is empty, then daily at a configured time (default 04:15 Europe/Berlin, after the GTFS import). One GET per dataset with `outputFormat=application/json&srsName=EPSG:4326`, a User-Agent naming the project, gzip; change detection by SHA-256 of the body (no validators upstream), unchanged → `skipped`. Sanity checks before replacing (non-empty, minimum share of the previous count, all points inside the scope bbox); a failed check keeps the old rows.
4. **Nearby rule for sparse layers:** all features within the request radius; when fewer than 3, add the nearest ones up to 3 within 2 km (`minResults` 3, `maxDistanceM` 2000). Ordered by distance. Same cell snapping as transit (ADR-0006), so the cell cache works unchanged.
5. **Version:** the id of the last import that changed the data, so the cache key and ETag change only when the data does.
6. **What the API sends** (decided with the user 2026-11-18):
   - Toilets: all 525, including the 16 open only at events or funerals (DATA_MODEL §7 #34), flagged `eventOnly` and shown with a note. `openingHours` as the source writes it (free text; 63 % are "24h"), plus `open24h`; no "open now" from free text in this phase. `fee` (euro, number), `payment`, `accessible` (`barrierefrei`), `mostlyAccessible` (`barrierearm`), `changingTable`, `kind` (`toilet` / `urinal`), `operator`.
   - Fountains: all 242, with `season` (`{fromMonth: 5, toMonth: 10}`, parsed from the source text) and `outOfService` (the source's restriction text, or null). The app greys a fountain out of season or out of service and says why.
7. **Mobile:** small icon markers (toilet, drop) drawn with `react-native-svg`, part of the overlap layout (task 7.9) but always below transit bubbles; a detail sheet per feature; layer switches on the map. Attribution "Geoportal Berlin (dl-de/zero-2-0)" in `meta.attribution` although the licence does not require it.

## Alternatives considered
- **One `amenities` layer with a `kind` field:** one switch for everything; but toilets and fountains have different fields and the user will want them separately. Two typed payloads keep the contract honest.
- **Keep the data in memory only (load at startup):** simplest, but every restart would depend on `gdi.berlin.de` being up, and nothing would be queryable for tests or tools. PostGIS is already there and makes the nearby query the same kind as transit's.
- **Proxy WFS per request (`bbox` filter):** live, but one upstream call per cell and cache miss, against rule 7's spirit ("poll from one place"), and the app would depend on an external service at request time.
- **Only features within the radius:** consistent with transit, but the map would often show no toilet at all (21 % / 47 % inside the ring, far more outside).
- **Hide event-only toilets / out-of-season fountains:** cleaner map, but the user chose to show them with a note.

## Consequences
- First real use of ADR-0008's multi-layer path end to end: registry with three providers, composite responses, per-layer versions in the cache key, mobile registry with more than one layer.
- New migration (`amenities` schema), new sqlc queries, two new config variables (import time, dataset URLs with defaults), new metrics (`punktchen_amenities_import_*`).
- The transit snapshot changes every ~14 s, so a combined request's ETag still changes as often as today (PROGRESS open question #4); amenity-only requests are cached until the next import.
- "Open now" for toilets stays out of scope; a later task could parse the common patterns ("Mo-Sa 07:00-22:00").
- Opening-hours text and restriction notes are German; the app shows them as they are.
