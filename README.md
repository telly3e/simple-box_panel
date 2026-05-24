# sing-box panel MVP

Lightweight local-first MVP for managing sing-box nodes, AnyTLS/Shadowsocks users, generated configs, subscriptions, relay endpoint overrides, and real or mock traffic accounting. Users can be disabled, edited, reset, or hard-deleted from the panel; Exit nodes can be paused or deleted without losing clarity about what the agent has applied.

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

After creating a node in the UI or API, run the agent with that node ID:

```powershell
go run ./apps/agent --node-id <exit-node-id> --agent-token <agent-token>
```

The agent token is generated per Exit node and is sent as `X-Sing-Panel-Agent-Token`. The API also accepts `Authorization: Bearer <agent-token>` for deployments that prefer bearer tokens. If the panel is behind HTTP Basic Auth, pass the same credentials in addition to the agent token:

```powershell
go run ./apps/agent --api-url https://panel.example.com --node-id <exit-node-id> --agent-token <agent-token> --api-basic-user admin --api-basic-pass 'choose-a-strong-password'
```

By default the Agent follows the node `stats_mode` from the API. New nodes use `mock`, so local development works without a running sing-box process. To test the real V2Ray API collector, patch a node to `v2ray-api` and point it at sing-box's local API listener:

```powershell
Invoke-RestMethod http://localhost:8080/api/exit-nodes/<exit-node-id> -Method Patch -ContentType application/json -Body '{"stats_mode":"v2ray-api","stats_api_listen":"127.0.0.1:10085"}'
go run ./apps/agent --node-id <exit-node-id> --agent-token <agent-token> --stats-mode auto
```

`--stats-mode mock` forces local mock traffic. `--stats-mode v2ray-api` forces gRPC collection from `stats_api_target`; `--v2ray-reset=true` makes each successful query report deltas by resetting counters after read.

The API stores SQLite data in `.runtime/sing-panel.db` by default. The agent writes generated sing-box configs to `.runtime/agent/<node-id>/sing-box.json`.

To validate the generated config before accepting a new version, install a compatible `sing-box` binary and enable config checks:

```powershell
go run ./apps/agent --node-id <exit-node-id> --agent-token <agent-token> --check-config --sing-box-bin sing-box
```

`--check-config` runs `sing-box check -c .runtime/agent/<node-id>/sing-box.json` after each config rewrite. It is off by default so the local MVP works without sing-box installed.

On a Linux Exit host, the agent can also restart a systemd-managed sing-box service after a new config passes validation:

```powershell
go run ./apps/agent --node-id <exit-node-id> --agent-token <agent-token> --check-config --sing-box-bin /usr/local/bin/sing-box --sing-box-service sing-box.service
```

The agent reports heartbeat status back to the panel, including the applied config version and the latest apply/collector error. In the node list, `applied vN / desired vN` means the agent has accepted the current config; `pending` or a visible error means the Exit host has not yet applied the latest desired version.

Pausing an Exit node removes it from generated subscriptions and makes the agent apply a valid sing-box config with no inbound listeners. Resuming the node restores the normal generated inbounds from the saved node settings.

Deleting an Exit node removes it from panel management and clears its historical traffic events. It does not remotely uninstall sing-box or modify files on the old host.

For smoke tests or staged rollouts, `--apply-only` makes the agent write, validate, optionally restart, and report heartbeat status without collecting traffic.

For a local real-stats smoke test, set an Exit node to `stats_mode=v2ray-api`, use certificate paths that are readable from the agent host, run the agent once with `--check-config`, then start sing-box with the generated server config:

```powershell
go run ./apps/agent --node-id <exit-node-id> --agent-token <agent-token> --stats-mode auto --check-config --sing-box-bin .runtime\bin\sing-box.exe --once
.\.runtime\bin\sing-box.exe run -c .runtime\agent\<exit-node-id>\sing-box.json
```

With sing-box running, another agent pass should connect to `stats_api_target` without a collector timeout. After sending traffic through a tracked user, the user's `used_bytes` should increase.

## Node subscriptions

Subscriptions are generated directly from nodes. Each node can independently enable AnyTLS, Shadowsocks, or both. Server configs only include the enabled inbound protocols, and user subscriptions only include the enabled client outbounds. Paused nodes are omitted from subscriptions and receive a no-inbound sing-box config from the agent.

Each user has a URL-safe subscription token. The subscription URL is:

```text
/sub/<subscription-token>
```

Resetting the token invalidates the old subscription URL without changing the user's protocol passwords or the Exit server config.

For relay-style deployments, enable `relay_enabled` on the node and set `relay_host`, `relay_anytls_port`, and/or `relay_ss_port`. The sing-box server still listens on the node's own `anytls_port`/`ss_port`, while subscriptions expose the relay host and relay ports to clients.

Generated AnyTLS server inbounds include `padding_scheme`. Each node can override it with one padding rule per line in the panel; leaving it empty uses sing-box's default AnyTLS padding scheme from the inbound documentation.

Shadowsocks nodes can choose the sing-box inbound methods listed in the official method table: `2022-blake3-aes-128-gcm`, `2022-blake3-aes-256-gcm`, `2022-blake3-chacha20-poly1305`, `none`, `aes-128-gcm`, `aes-192-gcm`, `aes-256-gcm`, `chacha20-ietf-poly1305`, and `xchacha20-ietf-poly1305`. The panel generates fixed-length base64 keys for 2022 methods: 16 bytes for `2022-blake3-aes-128-gcm`, and 32 bytes for the 256-gcm/chacha20 2022 methods. Server configs enable Shadowsocks inbound multiplex, and generated 2022 subscriptions use sing-box's multi-user client password form:

```text
<server-password>:<user-password>
```

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
.\scripts\smoke-local-e2e.ps1
.\scripts\smoke-staging-panel.ps1 -PanelUrl https://panel.example.com -BasicUser admin -BasicPass 'choose-a-strong-password'
bash scripts/smoke-staging-panel.sh --panel-url https://panel.example.com --basic-user admin --basic-pass 'choose-a-strong-password'
```

The Go test suite includes store tests, config generation tests, and HTTP API integration tests for the local user -> node -> subscription -> agent traffic loop.
The local smoke script starts a temporary panel API, agent, sing-box server, sing-box client, and HTTP origin, then verifies V2Ray API traffic accounting through a local Shadowsocks request.
The staging panel smoke script creates temporary records against a reachable panel, verifies API/config/subscription behavior, and removes them by default.
