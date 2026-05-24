# Staging deployment

This is the lightweight staging path for the panel container. It runs the Go API and built React UI in one container, stores SQLite under the `panel-data` Docker volume, and can optionally put Caddy in front for HTTPS and basic auth.

## 1. Prepare the host

Point a DNS record such as `panel.example.com` at the VPS public IP. Install Docker Engine and the Docker Compose plugin on the host.

Copy the example environment file and edit the domain and basic-auth hash:

```bash
cp deployments/.env.example deployments/.env
caddy hash-password --plaintext 'choose-a-strong-password'
```

Paste the generated hash into `PANEL_BASIC_AUTH_HASH`. Keep the plaintext password somewhere safe; the agent can use it with `--api-basic-pass` when it talks to the panel through Caddy. Each Exit node also has its own agent token; copy that token into the agent environment file.

## 2. Run without a reverse proxy

This is useful for private staging over SSH tunnels or a locked-down firewall:

```bash
docker compose -f deployments/docker-compose.yml up --build -d
curl http://127.0.0.1:8080/api/health
```

The public host port is controlled by `PANEL_HTTP_PORT`.

## 3. Run with HTTPS through Caddy

Open ports `80` and `443` on the VPS, then enable the `proxy` profile:

```bash
docker compose -f deployments/docker-compose.yml --profile proxy up --build -d
curl https://panel.example.com/api/health
```

Caddy obtains and renews certificates automatically for `PANEL_DOMAIN`. Keep basic auth enabled while the panel has no first-party login system.

## 4. Run the panel API smoke

From your local workstation or from the staging host, run the staging smoke script against the reachable panel URL. It creates a temporary user and node, verifies subscription generation, desired config generation, AnyTLS custom `padding_scheme`, Shadowsocks method switching, Shadowsocks multiplex, heartbeat status, and paused-node config, then deletes the temporary records.

Without Basic Auth:

```powershell
.\scripts\smoke-staging-panel.ps1 -PanelUrl http://127.0.0.1:8080
```

On Debian or other Linux hosts, the bash smoke script only needs `curl` and `python3`:

```bash
bash scripts/smoke-staging-panel.sh --panel-url http://127.0.0.1:8080
```

With Caddy Basic Auth:

```powershell
.\scripts\smoke-staging-panel.ps1 -PanelUrl https://panel.example.com -BasicUser admin -BasicPass 'choose-a-strong-password'
```

```bash
bash scripts/smoke-staging-panel.sh --panel-url https://panel.example.com --basic-user admin --basic-pass 'choose-a-strong-password'
```

Use `-SkipCleanup` only when you want to inspect the temporary records in the UI after a failure.

## 5. Back up SQLite

For a conservative backup, briefly stop the panel before copying the database file out of the named Docker volume:

```bash
mkdir -p backups
docker compose -f deployments/docker-compose.yml stop panel
docker run --rm -v sing-panel_panel-data:/data -v "$PWD/backups:/backups" alpine:3.22 sh -c 'cp /data/sing-panel.db /backups/sing-panel-$(date +%Y%m%d-%H%M%S).db'
docker compose -f deployments/docker-compose.yml start panel
```

Restore by stopping the panel and copying a chosen backup back to `/data/sing-panel.db` in the same volume.

## 6. Real traffic statistics check

After the panel is reachable, deploy an Exit node with a `sing-box` binary built with `with_v2ray_api` and `with_acme`, set the Exit node `stats_mode` to `v2ray-api`, and point `stats_api_listen` at the sing-box API listener, for example `127.0.0.1:10085`.

If the Exit node uses ACME DNS01 with Cloudflare, set the environment variable named in the node's Cloudflare token field on the agent host:

```bash
export CLOUDFLARE_API_TOKEN='...'
```

The panel stores only the environment variable name. The agent resolves `api_token_env` to sing-box's real `api_token` field before writing `sing-box.json`.

Run the agent on the Exit host with:

```bash
go run ./apps/agent --api-url https://panel.example.com --node-id <exit-node-id> --agent-token <agent-token> --stats-mode auto --check-config --sing-box-bin /usr/local/bin/sing-box --sing-box-service sing-box.service --api-basic-user admin --api-basic-pass 'choose-a-strong-password'
```

The agent will authenticate with its node token, fetch desired config, validate it with `sing-box check`, restart the configured systemd service after a valid config change, and upload per-user traffic deltas from V2Ray API stats. It also reports the applied config version and last agent error back to the panel so the node view can show whether the Exit host is current.

Real traffic acceptance checklist:

- Create or choose one enabled user with enough quota.
- Create or choose one enabled Exit node with Shadowsocks enabled, `stats_mode=v2ray-api`, and `stats_api_listen=127.0.0.1:10085`.
- If AnyTLS is enabled, verify the certificate mode and custom `padding_scheme` pass `sing-box check`.
- Run the agent once with `--apply-only --check-config` and confirm the node shows `applied vN / desired vN`.
- Start or restart `sing-box.service` with the generated config.
- Import the user's `/sub/<subscription-token>` config into a client and send traffic through the Exit node.
- Run or wait for the agent traffic collection pass.
- Confirm the user's `used_bytes` increases and the node has no `last_agent_error`.
- Pause the Exit node and confirm subscriptions omit it while the agent applies a no-inbound config.
- Resume the Exit node and confirm the desired/applied versions converge again.

For a persistent Exit host install, build the agent binary and use the included systemd template:

```bash
go build -trimpath -ldflags="-s -w" -o /opt/sing-panel/sing-panel-agent ./apps/agent
mkdir -p /etc/sing-panel /var/lib/sing-panel-agent
cp deployments/agent.env.example /etc/sing-panel/agent.env
cp deployments/sing-panel-agent.service /etc/systemd/system/sing-panel-agent.service
systemctl daemon-reload
systemctl enable --now sing-panel-agent
```

Edit `/etc/sing-panel/agent.env` before enabling the service.
