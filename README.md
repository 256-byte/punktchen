# LiveStop

> Working name. Repository: `punktchen`.

A map-first mobile app that shows **live departures** (bus, tram, U-Bahn, S-Bahn) directly on the stops around you in Berlin. Open the app, see every stop pole within ~500 m, each with its next departures per direction, counting down live.

**Status:** early development (Phase 0 — skeleton & tooling). Nothing runnable yet. See [`docs/PROGRESS.md`](docs/PROGRESS.md).

## Why

Departure boards are usually one stop at a time, behind a search. LiveStop puts all of them on the map at once.

It is also a portfolio project. The focus is the **backend architecture**:

- one shared poller per upstream feed, so upstream traffic does not grow with users,
- an immutable, versioned in-memory snapshot of schedule + realtime data,
- a geohash cell cache with ETags, so nearby users share one response,
- a contract-first API (OpenAPI) with generated Go and TypeScript code,
- a generic layer system, so other map data can be added later without touching the core.

## How it works

```
VBB GTFS (daily) ──► import ──► PostgreSQL + PostGIS
VBB GTFS-RT (15 s) ──► merge with schedule ──► Snapshot ──► /v1/nearby ──► mobile app
```

- **Backend:** Go, single binary (modular monolith), PostgreSQL + PostGIS, Docker Compose.
- **Mobile:** React Native + Expo, TypeScript. Countdowns are computed on the device from absolute timestamps, so the app keeps working offline.

Details: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/DATA_MODEL.md`](docs/DATA_MODEL.md), decisions in [`docs/adr/`](docs/adr/).

## Repository layout

| Path | Content |
|---|---|
| `api/` | `openapi.yaml` — the API contract |
| `backend/` | Go server (`cmd/`, `internal/`, `db/`, generated code in `gen/`) |
| `mobile/` | Expo app (`app/` screens, `src/` logic and UI) |
| `docs/` | plan, architecture, data model, progress, ADRs |

## How to run

Requirements: Docker Desktop (WSL2 on Windows), Go, Node.js (LTS or newer), Expo Go on your phone + a free Expo account.

**Backend + database** (PowerShell, from the repo root):

```powershell
Copy-Item .env.example .env        # optional; defaults work for local dev
docker compose up -d --build --wait
curl.exe http://localhost:8080/v1/health
```

- API: `http://localhost:8080` (reachable from the LAN for the phone)
- Metrics: `127.0.0.1:9090`, Postgres: `127.0.0.1:5432` (localhost only)
- All settings: [`.env.example`](.env.example)
- Stop: `docker compose down` (data stays in the `livestop_pgdata` volume; `down -v` deletes it)

### Run the app on your phone

The app runs in **Expo Go** and talks to the backend on your PC over the local network. No cable needed.

1. **Start the backend** (see above). Check: `curl.exe http://localhost:8080/v1/health`.
2. **Find the PC's LAN IP.** Run `ipconfig` and take the IPv4 address of the Wi-Fi adapter ("Wireless LAN adapter WLAN", e.g. `192.168.178.29`). Ignore `vEthernet`, `VMware` and `WSL` adapters.
3. **Point the app at it:**
   ```powershell
   Copy-Item mobile\.env.example mobile\.env
   # edit mobile\.env:  EXPO_PUBLIC_API_URL=http://192.168.178.29:8080
   ```
   Android emulator: use `http://10.0.2.2:8080` (the emulator's alias for the PC). `mobile/.env` is not committed.
4. **Same Wi-Fi.** Phone and PC must be on the same network — not a guest Wi-Fi, which isolates devices.
5. **Firewall.** Test from the phone's browser: `http://<PC-IP>:8080/v1/health` must show JSON.
   - Docker Desktop normally adds an inbound rule ("Docker Desktop Backend"), so this works out of the box.
   - If it times out, mark the home network as **Private** (Settings → Network & internet → Wi-Fi → network properties) and allow port 8080 there (PowerShell as admin):
     ```powershell
     New-NetFirewallRule -DisplayName "LiveStop API 8080" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow -Profile Private
     ```
   - Expo's dev server (Metro, port 8081) runs as Node.js; allow it when Windows asks on the first `expo start`.
6. **Expo account.** Expo Go only opens projects when the same (free) Expo account is signed in **in Expo Go and in Expo CLI** (`npx expo whoami`).
   - On Windows, the browser login of `npx expo login` may crash (the CLI passes the login URL unquoted to `cmd start`). Use `npx expo login --no-browser` (accounts with a password) or an access token from expo.dev in `$env:EXPO_TOKEN` (also works for Google/GitHub sign-ups). Never commit the token.
7. **Start Expo** and scan the QR code with the iPhone camera:
   ```powershell
   cd mobile
   npm install
   npx expo start
   ```
   The start screen shows **Backend: ok** with the server version and time. "Backend unreachable" means steps 2–5; "Backend not configured" means step 3.

`EXPO_PUBLIC_*` values are compiled into the bundle: after changing `mobile/.env`, restart with `npx expo start --clear`. Do the same after `npm install` — a running Metro server does not see newly installed packages ("Unable to resolve module …").

Mobile checks: `cd mobile; npm run typecheck; npm run lint; npm test`. After changing `api/openapi.yaml`: `npm run gen:api`. All commands: [`CLAUDE.md` §5](CLAUDE.md#5-commands).

## Data & licence

- Transit data: [VBB](https://www.vbb.de/) GTFS and GTFS-RT, licensed **CC BY 4.0**. Data: VBB (CC BY 4.0).
- Code: [MIT](LICENSE).
