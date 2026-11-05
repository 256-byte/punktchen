# 0002 — Modular monolith (single Go binary)

- **Status:** accepted
- **Date:** 2026-11-05

## Context
Importer, realtime poller, merger and API need to share in-memory state (snapshot) and are tiny in resource terms.

## Decision
One binary, strict package boundaries (`transit/static`, `transit/realtime`, `transit/departures`, `layer`, `api`, `geo`). Packages communicate through interfaces, not globals.

## Alternatives considered
Microservices + message broker (like VBB's own NATS setup): realistic for an operator, but pure overhead for one instance and would hide the interesting logic behind infrastructure.

## Consequences
Simple to run and debug. If ever needed, the poller can be split out because it only depends on interfaces.
