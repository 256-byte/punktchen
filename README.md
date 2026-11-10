# LiveStop

> Working name. Repository: `punktchen`.

A map-first mobile app that shows **live departures** (bus, tram, U-Bahn, S-Bahn) directly on the stops around you in Berlin. Open the app, see every stop pole within ~500 m, each with its next departures per direction, counting down live.

**Status:** in development. It runs locally: the backend serves live departures with realtime delays, and the app shows them on the map (Expo Go). Offline mode and UI polish are next. See [`docs/PROGRESS.md`](docs/PROGRESS.md).

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

## Performance

Measured on the dev machine (Windows, backend and PostGIS in Docker Compose) on Tuesday 2026-11-10, 08:04–08:09, the morning peak: 116,000 departures in the snapshot, live VBB realtime. Latency is measured on the same machine, without a network.

**Upstream.** One poller asks VBB 4 times a minute (limit: 7), whatever the number of users.

**One app.** The fetch policy ([`ARCHITECTURE.md` §4.2](docs/ARCHITECTURE.md)) decides when to ask:

| Situation | Requests per minute |
|---|---|
| Standing still, app open | 2 (data older than 30 s) |
| Moving, departures leaving, errors | at most 6 (never within 10 s of the last request) |
| App in the background | 0 |

**The server under 1000 apps.** `nearbyload` simulates apps standing still: one request every 30 s each, with the ETag of the last answer and gzip. Two runs of 2 minutes, 33 requests per second:

| | A crowd: 1000 apps within 300 m | Spread out: 1000 apps within 3 km |
|---|---|---|
| Cache cells used | 30 | 787 |
| Requests | 3,999 | 3,999 |
| Cell cache hit ratio | **93.5 %** | 13.0 % |
| Answers built | 257 | 3,459 |
| Latency of a 200 (p50 / p95) | 2.3 ms / 5.7 ms | 4.1 ms / 6.9 ms |
| Answer size, gzip (median) | 6.5 KB | 2.8 KB |
| 304 Not Modified | 0.6 % | 0.6 % |

What the numbers say:

- **The cache works where people are.** A crowd shares a few cells, and 93.5 % of its lookups are answered from the cache. People spread over the city rarely share a cell, but a miss is cheap: one indexed PostGIS query plus a lookup in the in-memory snapshot, about 4 ms.
- **A burst costs one build.** 1000 requests for one cell at the same moment build the answer once; the others wait for it (`TestNearbyLoad`).
- **304s are rare while realtime flows.** Every applied VBB feed makes a new snapshot version (every 14 s in these runs), and the ETag follows the version, so an app that asks every 30 s almost always gets new data. When VBB's feed paused (around midnight), the version changed once a minute and 50 % of the requests were 304s. An ETag from the content of a cell would do better; that is not built.
- **gzip saves about 91 %.** Alexanderplatz, 500 m: 74.7 KB → 6.5 KB. Wedding: 32.3 KB → 2.9 KB.
- **Snapshots are cheap to rebuild.** Loading the whole 2-hour window from the database: 1.9 s (at start and after an import). Moving the window each minute: 111 ms. Merging a new realtime feed into 116,000 departures: 46 ms. Readers never wait: a new snapshot replaces the old one atomically.
- **Memory:** about 250 MB for the backend at the morning peak.

Run it yourself against the Compose stack:

```powershell
go -C backend run ./internal/tools/nearbyload -users 1000 -spread 300
go -C backend run ./internal/tools/nearbyload -users 1000 -spread 3000
```

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
