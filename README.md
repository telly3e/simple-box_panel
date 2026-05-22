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

## Useful checks

```powershell
go test ./...
npm --prefix apps/web run build
```

The Go test suite includes store tests, config generation tests, and HTTP API integration tests for the local user -> node -> subscription -> agent traffic loop.
