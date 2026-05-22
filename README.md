# sing-box panel MVP

Lightweight local-first MVP for managing sing-box Entry/Exit nodes, AnyTLS/Shadowsocks users, generated configs, subscriptions, and mock traffic accounting.

## Prerequisites

- Go 1.22+
- Node.js 20+
- npm

## Local development

```powershell
go run ./apps/api
```

```powershell
npm --prefix apps/web install
npm --prefix apps/web run dev
```

To run the API as a single process that also serves the built React UI:

```powershell
npm --prefix apps/web run build
go run ./apps/api --web-dir apps/web/dist
```

After creating an Exit node in the UI or API, run the agent with that node ID:

```powershell
go run ./apps/agent --node-id <exit-node-id>
```

The API stores SQLite data in `.runtime/sing-panel.db` by default. The agent writes generated sing-box configs to `.runtime/agent/<node-id>/sing-box.json`.

To validate the generated config before accepting a new version, install a compatible `sing-box` binary and enable config checks:

```powershell
go run ./apps/agent --node-id <exit-node-id> --check-config --sing-box-bin sing-box
```

`--check-config` runs `sing-box check -c .runtime/agent/<node-id>/sing-box.json` after each config rewrite. It is off by default so the local MVP works without sing-box installed.

## Staging deployment

The root `Dockerfile` builds the React UI, builds the Go API, and serves both from one container. SQLite is stored under `/data`.

```powershell
docker compose -f deployments/docker-compose.yml up --build
```

The panel listens on `http://localhost:8080` and keeps data in the `panel-data` Docker volume.

## sing-box build with V2Ray API

Official sing-box docs list custom build tags and tell downstream packagers to read default tag files from `release/DEFAULT_BUILD_TAGS*` plus `release/LDFLAGS`. The V2Ray API page says that V2Ray API is not included by default, so this project keeps a helper script that reads the official default tags from a checked-out sing-box source tree and forces `with_v2ray_api` into the build.

```powershell
git clone https://github.com/SagerNet/sing-box D:\src\sing-box
.\scripts\build-sing-box.ps1 -SingBoxSource D:\src\sing-box -Output .runtime\bin\sing-box.exe
```

On Windows the script reads `release/DEFAULT_BUILD_TAGS_WINDOWS`; on Linux/macOS it reads `release/DEFAULT_BUILD_TAGS`. It also uses `release/LDFLAGS`.

## Useful checks

```powershell
go test ./...
npm --prefix apps/web run build
```

The Go test suite includes store tests, config generation tests, and HTTP API integration tests for the local user -> node -> subscription -> agent traffic loop.
