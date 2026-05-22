# sing-box panel MVP

Lightweight local-first MVP for managing sing-box Entry/Exit nodes, AnyTLS/Shadowsocks users, generated configs, subscriptions, and mock traffic accounting.

## Prerequisites

- Go 1.25.5+
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

The API flags can also be provided as environment-variable defaults:

- `SING_PANEL_ADDR` defaults to `:8080`
- `SING_PANEL_DB` defaults to `.runtime/sing-panel.db`
- `SING_PANEL_WEB_DIR` defaults to empty in local runs and `/app/web` in the container image

After creating an Exit node in the UI or API, run the agent with that node ID:

```powershell
go run ./apps/agent --node-id <exit-node-id>
```

If the panel is behind HTTP Basic Auth, pass the same credentials to the agent:

```powershell
go run ./apps/agent --api-url https://panel.example.com --node-id <exit-node-id> --api-basic-user admin --api-basic-pass 'choose-a-strong-password'
```

By default the Agent follows the node `stats_mode` from the API. New nodes use `mock`, so local development works without a running sing-box process. To test the real V2Ray API collector, patch an Exit node to `v2ray-api` and point it at sing-box's local API listener:

```powershell
Invoke-RestMethod http://localhost:8080/api/exit-nodes/<exit-node-id> -Method Patch -ContentType application/json -Body '{"stats_mode":"v2ray-api","stats_api_listen":"127.0.0.1:10085"}'
go run ./apps/agent --node-id <exit-node-id> --stats-mode auto
```

`--stats-mode mock` forces local mock traffic. `--stats-mode v2ray-api` forces gRPC collection from `stats_api_target`; `--v2ray-reset=true` makes each successful query report deltas by resetting counters after read.

The API stores SQLite data in `.runtime/sing-panel.db` by default. The agent writes generated sing-box configs to `.runtime/agent/<node-id>/sing-box.json`.

To validate the generated config before accepting a new version, install a compatible `sing-box` binary and enable config checks:

```powershell
go run ./apps/agent --node-id <exit-node-id> --check-config --sing-box-bin sing-box
```

`--check-config` runs `sing-box check -c .runtime/agent/<node-id>/sing-box.json` after each config rewrite. It is off by default so the local MVP works without sing-box installed.

For a local real-stats smoke test, set an Exit node to `stats_mode=v2ray-api`, use certificate paths that are readable from the agent host, run the agent once with `--check-config`, then start sing-box with the generated server config:

```powershell
go run ./apps/agent --node-id <exit-node-id> --stats-mode auto --check-config --sing-box-bin .runtime\bin\sing-box.exe --once
.\.runtime\bin\sing-box.exe run -c .runtime\agent\<exit-node-id>\sing-box.json
```

With sing-box running, another agent pass should connect to `stats_api_target` without a collector timeout. After sending traffic through a tracked user, the user's `used_bytes` should increase.

## Staging deployment

The root `Dockerfile` builds the React UI, builds the Go API, and serves both from one container. SQLite is stored under `/data`. The Compose setup supports an optional Caddy HTTPS reverse proxy profile and a sample environment file:

```powershell
Copy-Item deployments\.env.example deployments\.env
docker compose -f deployments/docker-compose.yml up --build
```

The panel listens on `http://localhost:8080` and keeps data in the `panel-data` Docker volume.

See `deployments/STAGING.md` for domain, HTTPS, basic-auth, SQLite backup, and real V2Ray API staging checks.

## sing-box build with V2Ray API

Official sing-box docs list custom build tags and tell downstream packagers to read default tag files from `release/DEFAULT_BUILD_TAGS*` plus `release/LDFLAGS`. The V2Ray API page says that V2Ray API is not included by default, and ACME certificate providers need `with_acme`, so this project keeps a helper script that reads the official default tags from a checked-out sing-box source tree and forces `with_v2ray_api` and `with_acme` into the build.

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
