# GTFS schedule fixtures

Data: VBB (CC BY 4.0).

| File | Content |
|---|---|
| `vbb-wedding-800m.zip` | Real subset of the VBB feed published 2026-11-05 (`https://unternehmen.vbb.de/gtfs`): every trip that serves a station with a pole within 800 m of U Wedding (52.5427, 13.3660), kept whole (all stop times, stops, routes, agencies, calendar rows), plus all poles of those 17 stations. 13,394 trips, 343,256 stop times, 837 stops, 2.7 MB. Contains every trip of the GTFS-RT fixtures in `../gtfsrt/`. DATA_MODEL §7 #22. |

Rebuilt from `backend/` with:

```powershell
go run ./internal/tools/gtfsexplore download -url https://unternehmen.vbb.de/gtfs -out data/gtfs-www/GTFS.zip
go run ./internal/tools/gtfsexplore subset -zip data/gtfs-www/GTFS.zip
```

A new feed changes trip ids, so rebuilding it also means re-recording the GTFS-RT fixtures. Each file must stay under 5 MB (CLAUDE.md rule 11).
