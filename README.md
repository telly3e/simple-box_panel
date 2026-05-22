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

## Useful checks

```powershell
go test ./...
npm --prefix apps/web run build
```

