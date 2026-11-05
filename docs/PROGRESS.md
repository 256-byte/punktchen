# Progress

> Updated by Claude Code at the end of every task. Newest session log entry on top.

## Current status

- **Active phase:** Phase 0 — Skeleton & tooling
- **Next task:** 0.2
- **Blocked by:** nothing

## Task status

| Phase | Task | Status | Notes |
|---|---|---|---|
| 0 | 0.1 Repo layout | done | Empty dirs kept via `.gitkeep`; remove each once the dir has real files. Added `.gitattributes` (LF) |
| 0 | 0.2 Docker Compose | todo | |
| 0 | 0.3 Go skeleton | todo | |
| 0 | 0.4 Migrations | todo | |
| 0 | 0.5 OpenAPI v0 + health | todo | |
| 0 | 0.6 sqlc | todo | |
| 0 | 0.7 Expo app | todo | |
| 0 | 0.8 Phone → backend networking | todo | |
| 0 | 0.9 CI | todo | |
| 0 | 0.10 Real commands in CLAUDE.md | todo | |

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

### 2026-11-05 — task 0.1
- Done: directory tree from CLAUDE.md §4 (empty dirs hold a `.gitkeep`), `.gitignore` (secrets, downloaded data, Go/Node/Expo build output, editor/OS files; `backend/testdata/**` re-included), `.editorconfig`, `.gitattributes`, MIT `LICENSE`, `README.md` (what / why / layout / run status).
- Extra (not in task text): `.gitattributes` with `eol=lf`, because this machine has `core.autocrlf=true`. Without it gofmt output, container shell scripts and the CI drift check (0.9) would break on CRLF.
- Tests (manual check): script compared every dir of CLAUDE.md §4 against the tree → none missing. `git check-ignore` → `.env`, `*.zip`, `*.pb`, `*.sql.gz`, `/data/`, `/backend/data/`, `node_modules/`, `.expo/`, `backend/bin/` ignored; `.env.example`, `backend/testdata/*.zip|*.pb`, `.vscode/extensions.json` not ignored.
- New deps: none.
- Data facts found: none.
- Note for 0.7: `mobile/` is not empty (`.gitkeep` files), so `create-expo-app` may refuse it. Scaffold in a temp dir and move the files in.
- Next: 0.2 Docker Compose.
