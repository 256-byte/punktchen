# 0004 — GTFS import into staging schema + atomic rename

- **Status:** accepted
- **Date:** 2026-11-05

## Context
The VBB feed is ~620 MB unzipped and changes a few times per week. Readers must never see partial data; sqlc needs static table names.

## Decision
Import into `gtfs_staging`, build indexes/derived tables, run sanity checks, then in one transaction rename `gtfs`→`gtfs_old`, `gtfs_staging`→`gtfs`, commit, drop `gtfs_old`. Import metadata lives in the non-swapped `meta` schema.

## Alternatives considered
- Per-import database + pointer (VBB's postgis-gtfs-importer approach): works, but needs dynamic connection switching.
- Truncate + reload in place: readers see empty/partial tables.

## Consequences
Disk temporarily holds two copies. Queries keep using `gtfs.*`. A failed import leaves old data untouched.
