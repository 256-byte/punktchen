# GTFS-RT fixtures

Recorded from the VBB GTFS-RT feed `https://production.gtfsrt.vbb.de/data`.
Data: VBB (CC BY 4.0).

| File | Content |
|---|---|
| `vbb-empty-stale.pb` | Real response of 2026-11-08 22:45 UTC: header only (version 2.0, FULL_DATASET), 0 entities, `header.timestamp` 2026-10-30T19:04:09Z (9 days old). DATA_MODEL §7 #14. |
| `vbb-20261108T225938Z-wedding.pb` … `vbb-20261108T230040Z-wedding.pb` | 4 live samples, 15 s apart (one identical sample dropped). Trimmed to the ~485 TripUpdates that touch a stop within 800 m of U Wedding (52.5427, 13.3660); each TripUpdate is kept whole (all its StopTimeUpdates). ~400 KB each. |

The live samples reference `trip_id`s and `stop_id`s of the static feed `https://www.vbb.de/gtfs` published 2026-11-05 (DATA_MODEL §7 #15). Tests that join them with static data need a matching static fixture.

Recorded and trimmed from `backend/` with:

```powershell
go run ./internal/tools/gtfsexplore record
go run ./internal/tools/gtfsexplore trim -zip data/gtfs-www/GTFS.zip -radius 800 data/gtfsrt/<files>
```

Each file must stay under 5 MB (CLAUDE.md rule 11).
