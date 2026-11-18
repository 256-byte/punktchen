# Amenity fixtures

Real answers of the Geoportal Berlin WFS, recorded on 2026-11-18 (task 9.2, DATA_MODEL §7 #34).
Data: Geoportal Berlin, Datenlizenz Deutschland – Zero – Version 2.0 (`https://www.govdata.de/dl-de/zero-2-0`).

| File | Content |
|---|---|
| `toiletten-20261118.json` | Public toilets, 525 features, 290 KB: `DefaultToiletsURL` in `internal/config/config.go` |
| `trinkwasserbrunnen-20261118.json` | Drinking fountains, 242 features, 116 KB: `DefaultFountainsURL` in `internal/config/config.go` |

Recorded unchanged (one line each) with:

```powershell
curl.exe -A "punktchen-research" -o backend/testdata/amenities/toiletten-YYYYMMDD.json "<DefaultToiletsURL>"
```

Each file must stay under 5 MB (CLAUDE.md rule 11).
