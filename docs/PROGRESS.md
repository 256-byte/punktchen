# Progress

> Updated by Claude Code at the end of every task. Newest session log entry on top.

## Current status

- **Active phase:** Phase 1 — Static GTFS (started 2026-11-08 with the user's OK while 0.9 is blocked)
- **Next task:** 1.2
- **Blocked by:** 0.9 only — GitHub Actions does not start jobs ("account is locked due to a billing issue"); user has a ticket open with GitHub

## Task status

| Phase | Task | Status | Notes |
|---|---|---|---|
| 0 | 0.1 Repo layout | done | Empty dirs kept via `.gitkeep`; remove each once the dir has real files. Added `.gitattributes` (LF) |
| 0 | 0.2 Docker Compose | done | |
| 0 | 0.3 Go skeleton | done | |
| 0 | 0.4 Migrations | done | |
| 0 | 0.5 OpenAPI v0 + health | done | layers/nearby/stations return 501 until 2.5 |
| 0 | 0.6 sqlc | done | sqlc runs in Docker (pinned image); `go generate ./...` needs Docker |
| 0 | 0.7 Expo app | done | Expo Go needs a signed-in Expo account on PC and phone (see session log) |
| 0 | 0.8 Phone → backend networking | done | Restart Expo with `--clear` after installing packages |
| 0 | 0.9 CI | blocked | Workflow merged (PR #1); jobs not started: GitHub account billing lock. Re-run when unlocked |
| 0 | 0.10 Real commands in CLAUDE.md | done | All commands run from the repo root; tested in Windows PowerShell 5.1 |
| 1 | 1.1 Data exploration | done | DATA_MODEL §7 #1–#20; all (verify) items answered. Default GTFS URL and RT rate limit updated |
| 1 | 1.2 Downloader | todo | |
| 1 | 1.3 Streaming import | todo | |
| 1 | 1.4 Indexes + in_scope | todo | |
| 1 | 1.5 Derived tables | todo | |
| 1 | 1.6 Sanity checks + swap | todo | |
| 1 | 1.7 Scheduler | todo | |
| 1 | 1.8 Nearby query | todo | |

Status values: `todo` · `in progress` · `done` · `blocked`. Add rows for the next phase when it becomes active.

## Decisions (short log, details in ADRs)

| Date | Decision | ADR |
|---|---|---|
| 2026-11-05 | Monorepo, Go backend, RN + Expo mobile, Postgres/PostGIS | 0001, 0009 |
| 2026-11-05 | Modular monolith, single binary | 0002 |
| 2026-11-05 | OpenAPI contract-first | 0003 |
| 2026-11-05 | Import via staging schema + atomic rename | 0004 |
| 2026-11-05 | Immutable snapshot behind atomic pointer | 0005 |
| 2026-11-05 | Geohash cell cache keyed by provider versions | 0006 |
| 2026-11-05 | Absolute timestamps, countdown on device | 0007 |
| 2026-11-05 | Generic Provider/Layer system, transit only | 0008 |
| 2026-11-08 | Default static feed `https://www.vbb.de/gtfs` (current, matches GTFS-RT); change detection by SHA-256 | — (config, DATA_MODEL §7 #15) |
| 2026-11-08 | GTFS-RT limit 7 req/min: CLAUDE.md rule 7 updated, poll interval floor 10 s | — (config, DATA_MODEL §7 #20) |
| 2026-11-08 | User: keep config/docs in line with current upstream facts without asking first | — |

## Open questions

| # | Question | Owner | Needed by |
|---|---|---|---|
| 1 | Server for deployment | user | later |
| 2 | Alert sources: format/terms of VBB Verkehrsmeldungen | Claude Code (research) | Phase 8 |
| 3 | Final app name | user | Phase 7 |

## Session log

<!-- Template:
### YYYY-MM-DD — task X.Y
- Done:
- Tests:
- New deps (+ why):
- Data facts found:
- Next:
-->

### 2026-11-08 — task 1.1
- Done:
  - `backend/internal/tools/gtfsexplore` (dev tool, not in the server binary): `download`, `static` (report), `stops` (list by id/name), `record` (GTFS-RT samples + one conditional GET), `rt` (RT vs. static report), `trim` (fixtures around a point).
  - Findings in DATA_MODEL §7 #1–#20; all **(verify)** marks removed (§1 Station, §3.2 `station_id`/`direction_id`, §5.2 ids, §6 route types).
  - Fixtures in `backend/testdata/gtfsrt/` (1.6 MB total): empty/stale feed + 4 live samples trimmed to 800 m around U Wedding.
- Data facts found (most important):
  - The configured static URL is stale (2026-08-21); `https://www.vbb.de/gtfs` is current and matches GTFS-RT 100 % (vs 46.9 %). → open question #4.
  - GTFS-RT rate limit is 7/min since 2026-11-02. → open question #5. Provider also asks for compressed responses and a User-Agent with contact info (for the Phase 4 poller).
  - GTFS-RT was empty with a 9-day-old header for ≥ 12 min; freshness must come from `header.timestamp`.
  - Stations must be grouped by `parent_station` or the DHID prefix: most bus poles have no parent.
  - RT sends absolute times (delay only on 24 %), TripUpdates only, sparse for BVG U-Bahn/tram/bus.
- Tests: exploration only (task allows "script/test, not shipped"). `go vet`, `gofmt`, golangci-lint 0 issues. Re-running `rt` on the trimmed fixtures reproduces the match rates (100 % trips, 99.7 % stops).
- Requests to VBB: 2 static downloads, ~20 GTFS-RT requests over 75 min (all ≤ 7/min).
- New deps (+ why): `github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs` v1.0.0 (in stack; decode GTFS-RT protobuf; also used by the Phase 4 poller), `google.golang.org/protobuf` (now direct; marshal trimmed fixtures).
- Follow-up (user decision, same day): `DefaultGTFSURL` → `https://www.vbb.de/gtfs`; `MinGTFSRTPollInterval` 5 s → 10 s (+ boundary tests 9 s rejected / 10 s accepted); CLAUDE.md rule 7, ARCHITECTURE §2/§3.1/§3.3/§3.7, PLAN 4.1, `.env.example` updated (7 req/min, gzip, User-Agent, freshness from `header.timestamp`).
- Next: 1.2.

### 2026-11-08 — task 0.10
- Done:
  - CLAUDE.md §5 rewritten: every command runs from the repo root (`go -C backend …`, `npm --prefix mobile …`), so each line can be pasted on its own. Covers Compose up/health/logs/down, `go vet`, full and `-short` tests, golangci-lint via Docker (v2.15.0, same as CI), `go generate` + `gen:api` (with "commit the generated files"), `npm ci`/typecheck/lint/test/start, `start -- --clear`, `expo install`, and the Phase 1.7 import trigger (marked as not implemented). Notes on Docker requirements, `-race`, phone setup.
  - `curl.exe` instead of `curl` (in Windows PowerShell 5.1 `curl` is an alias of Invoke-WebRequest).
  - `tests/node/core-boundary.test.ts`: ESLint warm-up moved to `beforeAll` (60 s timeout). The first lint loads all Expo plugins and took 7.8 s on a busy machine → the first case hit Vitest's 5 s default and failed. Would have been flaky in CI too.
- Tests (acceptance): ran every command verbatim in Windows PowerShell 5.1.26100 from the repo root: compose up `--wait` → healthy, health → `ok`, logs, vet, test (full + `-short`), golangci-lint 0 issues, `go generate` + `gen:api` → no diff, `npm ci` (on a scratch clone, because Expo was running in `mobile/`), typecheck, lint, test (Vitest 33 + Jest 7), `start -- --clear --help` (arg passing through npm.ps1 works), `cd mobile; npx expo install --check; cd ..`. Not run: `compose down`, `logs -f` and `start` without `--help` (long-running / would stop the running stack).
- New deps (+ why): none.
- Phase 0 status: all tasks done except 0.9 (blocked by the GitHub billing lock, see below).
- Next: Phase 1, task 1.1 (data exploration), once the user agrees to start it while 0.9 is blocked.

### 2026-11-08 — task 0.9
- Done:
  - `.github/workflows/ci.yml` (on push to master and on every PR; `contents: read`; concurrency cancels superseded runs). Actions pinned to commit SHAs with the release in a comment: checkout v7.0.1, setup-go v7.0.0, setup-node v7.1.0, golangci-lint-action v9.3.0 (all released ≥ 4 weeks ago).
    - `backend`: Go from `backend/go.mod`, `go vet ./...`, golangci-lint v2.15.0 (same version as local), `go test -race -count=1 ./...` (testcontainers uses the runner's Docker; `-race` needs cgo, which the Windows dev machine lacks → first race-detector run happens here).
    - `mobile`: Node from `mobile/.nvmrc` (26, same major as local), `npm ci`, `typecheck`, `lint`, `test`.
    - `contract`: `go generate ./...` (oapi-codegen + sqlc in Docker) and `npm run gen:api`, then fails with a hint and the diff if `git status --porcelain` is not empty.
  - `backend/.golangci.yml` (v2): standard linters + `gofmt` formatter, so unformatted Go fails CI. Generated code excluded (`lax`).
- Tests (local simulation): `golangci-lint config verify` OK, `run` → 0 issues; a deliberately mis-formatted file is reported by `gofmt`. `actionlint` (incl. shellcheck) clean. Fresh `git clone` (no `.env`, `.expo`, `node_modules`) → `npm ci`, typecheck, lint, Vitest 33 + Jest 7 green; regenerate everything → no diff.
- New deps (+ why): none. CI only.
- PR run (2026-11-08): https://github.com/256-byte/punktchen/pull/1 → all three jobs failed after 2 s without running a step. Annotation: "The job was not started because your account is locked due to a billing issue." Not a workflow problem; the user is resolving it with GitHub support.
- Decision (user, 2026-11-08): merge PR #1 anyway (CI config only, simulated locally), mark 0.9 `blocked`, continue with 0.10. 0.9 is done when a PR run is green after the account is unlocked (re-run with `gh run rerun`).
- Next: 0.10.

### 2026-11-08 — task 0.8
- Done:
  - `EXPO_PUBLIC_API_URL` from `mobile/.env` (template `mobile/.env.example`; `.env` is git-ignored). Read once in `src/lib/api.ts` as a literal `process.env.EXPO_PUBLIC_API_URL` (Expo inlines it at bundle time) and validated by `src/core/apiBaseUrl.ts` (pure: http(s) only, no query/fragment/credentials, port 1–65535, trailing slashes removed; regex instead of `new URL()` because RN's URL polyfill is incomplete).
  - `src/api/client.ts`: openapi-fetch client typed by the generated `paths`; 5 s timeout via AbortController (an unreachable LAN IP otherwise hangs ~1 min on iOS) that also honours the caller's abort signal; errors become `ApiError`. `fetchHealth()`.
  - `src/api/health.ts`: `useHealth()` (TanStack Query, no retry). `app/_layout.tsx` provides a `QueryClient` (persistence comes with Phase 3/5).
  - `src/ui/BackendStatus.tsx` on the start screen: "Backend: ok/degraded/down" + version + server time + URL; "Backend unreachable" with the error and a network hint; "Backend not configured" when the env var is missing/invalid; "Check again" button.
  - README "Run the app on your phone": LAN IP via `ipconfig`, `mobile/.env`, Android emulator `10.0.2.2`, same Wi-Fi (no guest network), firewall (Docker Desktop's inbound rule; fallback `New-NetFirewallRule` for 8080 on the Private profile), Expo account + Windows login workarounds, `expo start --clear` after env changes.
- Findings:
  - Docker Desktop already has an inbound allow rule for `com.docker.backend.exe` (Public profile), which is what accepts the published port 8080 → no extra firewall rule was needed on this PC. The Wi-Fi is classified as "Public" by Windows; the README recommends "Private".
  - `npm audit`: 31 moderate/low advisories, all in transitive build/test tooling (e.g. `minimatch`, `glob`, `deepmerge` via Expo CLI / Jest). `npm audit fix --force` would break the SDK-pinned versions; left as is, revisit on the next Expo SDK upgrade.
  - Typecheck does not depend on the git-ignored `expo-env.d.ts` / `.expo/` (checked with a tsconfig that excludes them), so CI can run it on a fresh clone.
- Tests: Vitest 33 (20 new for `parseApiBaseUrl`: valid/normalized URLs, missing value, 8 invalid forms, port range). Jest 7 (6 new for `BackendStatus` with a fake fetch: ok + requested URL, down, unreachable + hint, timeout, "Check again" refetch, missing config without network). `typecheck`, `lint` green. `expo export --platform ios` with `.env` → bundle contains the configured URL.
- New deps (+ why): `openapi-fetch` (stack; typed client from the generated types), `@tanstack/react-query` (stack; request state now, cache/persistence later).
  - A Metro dev server that was already running while packages were installed fails with "Unable to resolve module ./useQueries.js from …/@tanstack/react-query/…" (its file map missed the new files). Fix: stop it and run `npx expo start --clear`.
- Manual check (acceptance): iPhone in Expo Go on the home Wi-Fi shows "Backend: ok" with version and server time from `http://192.168.178.29:8080/v1/health` (2026-11-08).
- Next: 0.9 CI.

### 2026-11-07 — task 0.7
- Done:
  - `mobile/`: Expo SDK 58 (expo 58.0.23, React Native 0.88.1, React 19.3.0, TypeScript 6.0), Expo Router with routes in `mobile/app/` (CLAUDE.md §4; the SDK 58 template would use `src/app/`). Placeholder `app/index.tsx` + Stack layout. iOS and Android only (`platforms`), typed routes on.
  - Started from `create-expo-app --template default` in a scratch dir and kept only what is needed: no example screens, no Expo-branded assets/icons (app icon comes in Phase 7), no web target, no Reanimated/Gesture Handler/bottom-sheet yet (added in the phase that uses them).
  - `tsconfig.json`: Expo base + `strict`, `noUncheckedIndexedAccess`, `noFallthroughCasesInSwitch`, alias `@/*` → `src/*`.
  - ESLint 9 flat config: `eslint-config-expo` + `eslint-config-prettier`; `no-restricted-imports` for `src/core/**` forbids `react`, `react-native`, `expo` (and subpaths), `react-native-*`, `expo-*`, `@expo/*`, `@react-native/*`, `@react-navigation/*`.
  - Prettier (single quotes, width 100). `npm run lint` = ESLint + `prettier --check`.
  - Vitest for `src/core` + `tests/node`; Jest (jest-expo preset) + RNTL 14 for UI, ignoring those two dirs. `npm test` runs both.
  - `npm run gen:api`: openapi-typescript → `src/api/schema.gen.ts` (marked generated in `.gitattributes`).
  - ARCHITECTURE §4.6 (tooling and tests).
- Findings:
  - TypeScript 6 no longer loads `@types/*` automatically (`types` defaults to `[]`). Jest tests import `describe/it/expect` from `@jest/globals` (same style as Vitest); `@types/jest` not needed.
  - openapi-typescript 7.13.0 (latest) declares `peer typescript ^5.x`; Expo SDK 58 ships TS 6. npm `overrides` lets it use the project's TS 6; output is byte-identical across runs and typechecks.
  - npm resolved the optional `react-dom` peer (via expo-router's web deps) to 19.3.1, which needs react 19.3.1 → ERESOLVE. Pinned `react-dom` 19.3.0 like the Expo template does.
  - npm 11 blocks install scripts by default. `@parcel/watcher` (via jest-haste-map) wants `node-gyp rebuild` as a fallback; the prebuilt `@parcel/watcher-win32-x64` is installed, so the script is denied in `allowScripts`.
- Tests: `npm run typecheck`, `npm run lint`, `npm test` green. Vitest: 13 cases for the core boundary rule (9 forbidden imports rejected, 3 allowed, rule inactive outside `src/core`) run against the real ESLint config. Jest: HomeScreen renders. `gen:api` twice → identical sha256. `npx expo export --platform ios` bundles (Hermes, 2.5 MB). `npx expo-doctor` 20/20 checks passed.
- New deps (+ why):
  - runtime: `expo`, `expo-router`, `react`, `react-native` (stack); `expo-constants`, `expo-linking`, `@expo/metro-runtime`, `react-native-screens`, `react-native-safe-area-context` (required peers of expo-router); `react-dom` (peer pin, see findings).
  - dev: `typescript`, `@types/react`, `eslint`, `eslint-config-expo`, `eslint-config-prettier`, `prettier` (lint/format); `vitest` (core tests); `jest`, `jest-expo`, `@jest/globals`, `@testing-library/react-native`, `test-renderer` (UI tests; test-renderer is RNTL 14's required peer); `openapi-typescript` (API types).
  - Expo Go (SDK 58) only opens a project when the same Expo account is signed in both in Expo Go and in Expo CLI ("You need to be signed in to Expo Go and Expo CLI"). A free account is enough.
  - `npx expo login` (browser flow) crashes on Windows: the CLI passes the login URL unquoted to `cmd /c start`, so the `&` in the URL splits the command. Workarounds: `npx expo login --no-browser` (accounts with a password), or an access token in `EXPO_TOKEN` (works for Google/GitHub sign-up accounts). Never commit the token.
  - Metro picked the right LAN address (WLAN `192.168.178.29`, not the VMware adapters). Windows Firewall already had an inbound allow rule for Node.js.
- Manual check (acceptance): `npx expo start` on the PC, QR code scanned on the iPhone → app opens in Expo Go and shows "LiveStop / Live departures around you" (2026-11-08).
- Next: 0.8 phone → backend networking (README section on LAN IP + firewall can reuse the findings above).

### 2026-11-07 — task 0.6
- Done:
  - `backend/sqlc.yaml`: PostgreSQL engine, schema = `db/migrations` (sqlc applies the golang-migrate `*.up.sql` files in order, ignores `*.down.sql`), queries = `db/queries`, output `gen/queries` (package `queries`, pgx/v5). Nullable columns → `*T`, `timestamptz` → `time.Time`, `jsonb` → `json.RawMessage`, `url`/`sha256` renamed to `URL`/`SHA256`, strict function checks on.
  - First query `db/queries/feed_imports.sql`: `LastSuccessfulFeedImport(source)` → newest `succeeded` row of `meta.feed_imports` (input for change detection in Phase 1; no importer logic added).
  - `db/generate.go`: `//go:generate go run ../internal/tools/sqlc generate`. `go generate ./...` now runs oapi-codegen and sqlc.
  - `internal/tools/sqlc`: small wrapper that runs `docker run sqlc/sqlc:1.31.1@sha256:70f5…5537` with the backend dir mounted (on Linux as the calling user, so CI-generated files are not owned by root); forwards sqlc's exit code.
- Why sqlc in Docker and not as a `go tool` like oapi-codegen: sqlc's PostgreSQL parser (`pg_query_go`) needs cgo + a C toolchain, which the Windows dev machine does not have. Docker is already required (Compose, testcontainers); pinning by digest makes output identical locally and in CI.
- Tests: integration test `db/queries_test.go` (migrated PostGIS container): `pgx.ErrNoRows` on empty table; with 5 rows picks the newest succeeded row of the right source (ignores newer failed/running rows and other feeds) and maps every type correctly. Unit test for the wrapper's root lookup (caught: relative path was returned; now always absolute). `go generate ./...` twice → identical hashes. Broken query (unknown column) → `go generate` fails with sqlc's error message. `go vet` / `gofmt` / `golangci-lint` clean, full `go test ./...` green.
- New deps (+ why): none in go.mod. Tool: Docker image `sqlc/sqlc:1.31.1` (in stack).
- Note for 0.9 / 0.10: CI contract job and CLAUDE.md §5 must state that `go generate ./...` needs Docker.
- Next: 0.7 Expo app (Node.js must be installed on the dev machine first).

### 2026-11-06 — task 0.5
- Done:
  - `api/openapi.yaml` (OpenAPI 3.1.0) v0: `GET /v1/health`, `/v1/layers`, `/v1/nearby`, `/v1/stations/{stationId}` with schemas from DATA_MODEL §5 (`Health`, `LayerInfo`, `NearbyResponse`, `ResponseMeta`, `LayerMeta`, `TransitLayer`, `Station`, `Pole`, `Direction`, `Departure`, `Mode`, `Status`, `Error`). Nullable fields use 3.1 `type: [X, "null"]`. ETag/Cache-Control headers, `If-None-Match`, `304`, `400`, `404` and a `default` Error response on every operation.
  - Codegen: oapi-codegen v2.8.2 as a Go tool dependency (`tool` directive in go.mod, run via `go tool`), config `backend/internal/api/oapi-codegen.yaml` (models + std-http-server + strict-server, prefixed enum names), `//go:generate` in `internal/api/generate.go`, output `backend/gen/openapi/openapi.gen.go`.
  - `internal/api`: `Server` implements the generated `StrictServerInterface`. `/v1/health` → `{status, version, time}`; `down` if the DB ping (1 s timeout) fails. The other three return 501 `not_implemented` via the `default` response. Param binding errors → 400 `invalid_argument`, unknown path → 404 `not_found`, handler errors → 500 `internal` (all in the `Error` format).
  - `platform.Berlin` (Europe/Berlin, tzdata embedded via `time/tzdata`) for API timestamps.
  - Placeholder handler removed from `cmd/server`.
  - ARCHITECTURE §3.7: health always 200, Docker healthcheck = liveness, Phase 0 status rule.
- Contract decisions (within ADR-0003/0008, no new ADR):
  - `NearbyResponse.layers` is an object with one optional, typed property per layer (`transit: TransitLayer`), each payload with a `layer` discriminator field, instead of a `oneOf` union map. Same JSON as DATA_MODEL §5.2, simpler generated Go/TS types, new layers are additive.
  - `Station.distanceMeters` is optional (only meaningful on `/v1/nearby`); `ResponseMeta.cell` likewise.
  - `Error.code` is a string with documented values, not an enum, so adding codes never breaks clients.
  - Temporary 501s use the `default` response instead of a dedicated `501` in the contract.
- Tests: `internal/api` unit tests with fake DB pinger + fixed clock: health ok (time `2026-11-05T14:32:00+01:00`, whole seconds), health with DB down (200 + `down`), 501 JSON for 4 paths, 400 JSON for 3 bad param cases, 404 JSON. `go generate ./...` twice → identical output. `go vet` / `gofmt` / `golangci-lint` clean, full `go test ./...` green.
- Manual check (acceptance): `curl localhost:8080/v1/health` → `200 {"status":"ok","time":"2026-11-06T17:27:58+01:00","version":"dev"}`. `docker compose stop db` → `down`; start db → `ok` again (pool reconnects).
- New deps (+ why): `github.com/oapi-codegen/oapi-codegen/v2` v2.8.2 (tool, in stack; OpenAPI 3.1 support since v2.8.0, "initial"), `github.com/oapi-codegen/runtime` v1.8.0 (imported by generated code for param binding).
- Not done here (by plan): request validation against the spec (lat/lng ranges, radius 100–1000, unknown layer → 400) and contract tests → task 2.5. TypeScript generation → 0.7.
- Next: 0.6 sqlc.

### 2026-11-06 — task 0.4
- Done:
  - `backend/db/migrations/0001_meta.{up,down}.sql`: `CREATE EXTENSION IF NOT EXISTS postgis`, schema `meta`, table `meta.feed_imports` with CHECK constraints (status values, `finished_at` ⇔ not running, `error` only on failure, sha256 hex) + index `(source, started_at DESC)`. Down drops only the table (schema `meta` holds the migration table; postgis may be used by gtfs data).
  - `backend/db` package embeds the migrations (`//go:embed`), so the binary/image needs no extra files.
  - `platform.Migrate`: golang-migrate on top of the app's pgx pool (`stdlib.OpenDBFromPool`, pgx5 driver, iofs source). Version table in `meta.schema_migrations` (creates schema `meta` first, the driver does not). `ErrNoChange` → "up to date". Dirty DB → startup fails with a clear message. Stops between migrations on SIGTERM. golang-migrate logs go through slog (`component=migrate`).
  - `cmd/server`: runs migrations after connecting, before opening ports (so "healthy" implies a migrated schema).
  - DATA_MODEL §3.3: status values and constraints of `feed_imports` documented.
- Tests: `go test ./...` green, `go vet` / `gofmt` / `golangci-lint` clean. Unit: migration file conventions (names, versions 1..n without gaps, every up has a down). Integration (testcontainers): apply twice → no error, version 1 not dirty, table in `meta` not `public`, postgis installed; `feed_imports` constraints (4 valid, 5 invalid rows); down then up; a broken migration is fully rolled back (verifies the implicit-transaction claim) and the next run fails with "dirty".
- Manual check (acceptance): existing dev volume → first start logs `1/u meta` and `from_version 0 → to_version 1`; `docker compose restart backend` → `database schema up to date, version 1`; container healthy.
- New deps (+ why): `github.com/golang-migrate/migrate/v4` v4.20.1 (in stack; pgx v5 driver + iofs source).
- Findings: Postgres runs a multi-statement query as one implicit transaction → migration files must not contain statements that cannot run in a transaction (e.g. `CREATE INDEX CONCURRENTLY`); noted in `db/db.go`. Each DB integration test starts its own container (~4–5 s each, platform package now ~40 s). If this grows, switch `pgtest` to one shared container with a fresh database per test.
- Next: 0.5 OpenAPI v0 + health.

### 2026-11-06 — task 0.3
- Done:
  - `internal/config`: `Load(lookup)` reads all `LIVESTOP_*` vars, applies defaults (same as `.env.example`), validates every value and returns **all** errors at once. `LIVESTOP_DATABASE_URL` is required. `Secret` type keeps the DB URL out of `fmt`/`slog` output; `Config.LogValue()` is the startup summary with the password redacted (`xxxxx`). HTTP and metrics addr must differ. GTFS-RT poll interval floor: 5 s (`MinGTFSRTPollInterval`, safety margin below VBB's 60 req/min).
  - `internal/geo`: `BBox` + `ParseBBox` (for `LIVESTOP_SCOPE_BBOX`).
  - `internal/platform`: `NewLogger` (slog JSON), `NewPool` (pgxpool, `application_name=livestop`, ping with 10 s timeout → fail fast), `NewMetricsRegistry` + `MetricsHandler` (own registry, Go + process collectors), `Listen` (binds early → port conflicts fail at startup) + `Serve` (runs API and metrics servers, graceful shutdown on ctx cancel, 8 s timeout, then force close).
  - `internal/platform/pgtest`: starts a throwaway PostGIS container via testcontainers (skipped with `-short`). Reused by DB tests from 0.4 on.
  - `cmd/server`: config → logger → DB → listeners → serve; SIGINT/SIGTERM via `signal.NotifyContext`; `version` via `-ldflags -X main.version` (Dockerfile `ARG VERSION`). Health handler is still a temporary stub in `main.go` (replaced in 0.5).
- Tests: `go test ./...` green. Unit: config (defaults, all set, empty = default, 14 invalid values, all errors reported, secret never printed via slog/fmt/error), bbox parsing, logger JSON + level filter, metrics output, port-in-use, graceful shutdown (in-flight request finishes, `Serve` returns nil), shutdown timeout. Integration (testcontainers, PostGIS 18-3.7): pool connects, `application_name` set; unreachable DB fails fast without leaking the password. `go vet`, `gofmt`, `golangci-lint` 2.15 (via Docker image) → 0 issues.
- Manual check (acceptance): `docker compose up -d --build --wait` → healthy; log shows config summary with `postgres://livestop:xxxxx@db:5432/...`; `docker compose stop backend` → logs `shutting down` (reason `terminated signal received`) and `stopped`, exit code 0. On the host: bad config → one error line listing every bad var, exit 1; DB down → immediate error, exit 1.
- New deps (+ why): `github.com/jackc/pgx/v5` v5.12.0 (DB pool, in stack), `github.com/prometheus/client_golang` v1.25.0 (metrics, in stack), `github.com/testcontainers/testcontainers-go` + `modules/postgres` v0.45.0 (DB integration tests, in stack; test-only).
- Machine notes: `go test -race` needs cgo/gcc, not available on this Windows PC → race detector runs in CI (0.9). `golangci-lint` is not installed locally; ran it with `docker run golangci/golangci-lint` (to be put into CLAUDE.md §5 in 0.10).
- Next: 0.4 Migrations.

### 2026-11-06 — task 0.2
- Done: `docker-compose.yml` with `db` (`postgis/postgis:18-3.7`, volume `pgdata`, `pg_isready` healthcheck, `shm_size: 512mb`) and `backend` (multi-stage `backend/Dockerfile`: `golang:1.27-alpine` → `gcr.io/distroless/static-debian13:nonroot`, static binary, 15.8 MB image). `.env.example` with every `LIVESTOP_*` var. README "How to run" for the backend.
- Ordering note: 0.2 needs a backend binary to be "healthy", but the Go skeleton is 0.3. Added a stdlib-only placeholder `backend/cmd/server/main.go` (`GET /v1/health` → `{"status":"ok"}`, SIGTERM shutdown) and `backend/go.mod` (module `github.com/256-byte/punktchen/backend`, go 1.27). 0.3 replaces the placeholder; 0.5 moves `/v1/health` behind generated handlers.
- Healthcheck: distroless has no shell/curl, so the binary has a `healthcheck` subcommand (`/livestop healthcheck`), used by the Dockerfile `HEALTHCHECK`.
- Config vars introduced (to be parsed + validated in 0.3): `LIVESTOP_DB_USER|PASSWORD|NAME|HOST_PORT` (compose only), `LIVESTOP_DATABASE_URL`, `LIVESTOP_HTTP_ADDR`, `LIVESTOP_METRICS_ADDR`, `LIVESTOP_LOG_LEVEL`, `LIVESTOP_DEV`, `LIVESTOP_GTFS_URL`, `LIVESTOP_GTFS_IMPORT_TIME`, `LIVESTOP_SCOPE_BBOX`, `LIVESTOP_GTFSRT_URL`, `LIVESTOP_GTFSRT_POLL_INTERVAL`. In compose, `DATABASE_URL`/`HTTP_ADDR`/`METRICS_ADDR` are set by the compose file and override `.env`.
- Ports: 8080 on all interfaces (phone over LAN), 9090 and 5432 on 127.0.0.1 only.
- Tests (manual check): `docker compose up -d --build --wait` → both `healthy`. `curl localhost:8080/v1/health` → 200 `{"status":"ok"}`. Postgres 18.6, PostGIS 3.7.0 available, data dir `/var/lib/postgresql/18/docker` (inside the volume). Row written → `down` (1 s, clean SIGTERM) → `up` → row still there. Container runs as `nonroot`. `go vet` + `gofmt` clean.
- New deps: none (Go stdlib only). Images: `postgis/postgis`, `golang`, `distroless/static`.
- Infra facts: Postgres 18 images moved PGDATA to `/var/lib/postgresql/<major>/docker` → volume mounted at `/var/lib/postgresql`. The postgis image already creates the `postgis` extension in `POSTGRES_DB`; migration 0001 (task 0.4) should still `CREATE EXTENSION IF NOT EXISTS` so it does not depend on the image.
- Machine note: Node.js is not installed on the dev PC → needed before 0.7.
- Next: 0.3 Go skeleton.

### 2026-11-05 — task 0.1
- Done: directory tree from CLAUDE.md §4 (empty dirs hold a `.gitkeep`), `.gitignore` (secrets, downloaded data, Go/Node/Expo build output, editor/OS files; `backend/testdata/**` re-included), `.editorconfig`, `.gitattributes`, MIT `LICENSE`, `README.md` (what / why / layout / run status).
- Extra (not in task text): `.gitattributes` with `eol=lf`, because this machine has `core.autocrlf=true`. Without it gofmt output, container shell scripts and the CI drift check (0.9) would break on CRLF.
- Tests (manual check): script compared every dir of CLAUDE.md §4 against the tree → none missing. `git check-ignore` → `.env`, `*.zip`, `*.pb`, `*.sql.gz`, `/data/`, `/backend/data/`, `node_modules/`, `.expo/`, `backend/bin/` ignored; `.env.example`, `backend/testdata/*.zip|*.pb`, `.vscode/extensions.json` not ignored.
- New deps: none.
- Data facts found: none.
- Note for 0.7: `mobile/` is not empty (`.gitkeep` files), so `create-expo-app` may refuse it. Scaffold in a temp dir and move the files in.
- Next: 0.2 Docker Compose.
