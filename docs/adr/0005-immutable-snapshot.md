# 0005 — Immutable snapshot behind an atomic pointer

- **Status:** accepted
- **Date:** 2026-11-05

## Context
Departures are read on every request and rebuilt every few seconds from schedule + realtime.

## Decision
Build a new immutable `Snapshot` on each change and publish it with `atomic.Pointer[Snapshot]`. Each snapshot has a monotonically increasing `Version`.

## Alternatives considered
Mutex-protected mutable map (lock contention, easy to get wrong), Redis (extra infra for a single instance).

## Consequences
Lock-free reads, trivially race-free. Memory: two snapshots briefly coexist (fine at Berlin scale). `Version` doubles as cache/ETag key input.
