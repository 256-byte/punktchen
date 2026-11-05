# 0007 — Absolute timestamps, countdown on the device

- **Status:** accepted
- **Date:** 2026-11-05

## Context
Countdowns change every second; connectivity on the move is unreliable.

## Decision
API returns absolute ISO-8601 timestamps with offset (scheduled, predicted). The app computes "N min" locally with a single global ticker.

## Alternatives considered
Server-computed minutes: wrong as soon as the response ages, forces frequent polling, breaks offline mode.

## Consequences
Offline mode is nearly free. Device clock skew matters → client may compare `meta.generatedAt` with local time and correct if skew > 30 s.
