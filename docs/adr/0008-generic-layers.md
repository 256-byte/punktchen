# 0008 — Generic Provider/Layer system, transit only

- **Status:** accepted
- **Date:** 2026-11-05

## Context
The map may later show other data (public toilets, elevator status, disruption alerts) from multiple sources.

## Decision
Backend: `layer.Provider` interface + registry; `/v1/nearby?layers=...` composes per-layer typed payloads (OpenAPI discriminator `layer`). Mobile: layer registry with per-layer selector, marker and sheet components. Only the `transit` layer is implemented now.

## Alternatives considered
Transit-specific API now, generalize later: cheaper today, but would force breaking API and map-screen changes later.

## Consequences
Small upfront abstraction. Rule: no speculative second layer until its phase is approved.
