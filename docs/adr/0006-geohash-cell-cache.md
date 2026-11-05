# 0006 — Geohash cell cache keyed by provider versions

- **Status:** accepted
- **Date:** 2026-11-05

## Context
Many users request nearly identical areas; responses depend only on location and data version.

## Decision
Snap requests to geohash precision-7 cells (~150 m), query around the cell centre with radius expanded by the cell half-diagonal. Cache key `(layers, cell, radiusBucket, providerVersions)`; ETag derived from it. Library: `mmcloughlin/geohash` (pure Go).

## Alternatives considered
H3 (`uber/h3-go`): nicer hexagons, but needs cgo, which complicates building on Windows and in minimal containers. Can be revisited.

## Consequences
No invalidation code: a new version simply produces new keys. Responses may include stops slightly beyond 500 m from the real user position; the client may sort/filter by its own distance.
