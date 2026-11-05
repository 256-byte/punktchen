# 0003 — OpenAPI contract-first

- **Status:** accepted
- **Date:** 2026-11-05

## Context
Two codebases in different languages must agree on the API shape.

## Decision
`api/openapi.yaml` is the source of truth. Go server types/interfaces via `oapi-codegen`; TS types via `openapi-typescript`, client via `openapi-fetch`. Generated code is committed and CI fails on drift.

## Alternatives considered
Hand-written types on both sides (drifts), gRPC (not a good fit for a mobile JSON/HTTP-cache flow), GraphQL (unneeded flexibility, harder HTTP caching).

## Consequences
Every API change starts in the YAML. Contract tests validate real responses against the spec.
