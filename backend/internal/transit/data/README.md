# VBB line colours

`linienfarben.csv` is VBB's official colour list for regional, S-Bahn and U-Bahn lines (plus a few city tram and bus lines in Brandenburg). It is embedded into the backend and used when a route has no `route_color` (DATA_MODEL §7 #25).

- Source: https://unternehmen.vbb.de/digitale-services/datensaetze/, section "Linienfarben" ("Letzte Aktualisierung: April 2026").
- File: `linienfarben.zip` (2,540 bytes, Last-Modified 2026-09-09), containing `20260828-linienfarben.csv`. Downloaded 2026-11-09.
- License: CC BY 4.0, © Verkehrsverbund Berlin-Brandenburg GmbH (VBB). The API's attribution "Data: VBB (CC BY 4.0)" covers it.
- Changes from the original: converted from ISO-8859-1 to UTF-8 and from CRLF to LF line endings. Content unchanged.

This is the one exception to CLAUDE.md rule 11 (no downloaded data in git): the file is tiny reference data, not a feed.

## Update

1. Download `linienfarben.zip` from the page above and unzip it.
2. Convert: `iconv -f ISO-8859-1 -t UTF-8 <file>.csv | tr -d '\r' > linienfarben.csv`.
3. Update the file name and dates above, run `go -C backend test ./internal/transit/` and check the colour coverage in the test log.

## Format

Semicolon-separated, with a header. Columns used: `Linie` (= `route_short_name`), `agency_id`, `route_Type` and `background_R/G/B`. `background_Hex` is empty on 72 of 155 rows, so the colour is built from R/G/B. Rows without an `agency_id` (`Tram`, `Fähre`, `Bus`) are generic mode colours, not line colours, and are not used.
