# Staging deployment

This is the lightweight staging path for the panel container. It runs the Go API and built React UI in one container, stores SQLite under the `panel-data` Docker volume, and can optionally put Caddy in front for HTTPS and basic auth.

## 1. Prepare the host

Point a DNS record such as `panel.example.com` at the VPS public IP. Install Docker Engine and the Docker Compose plugin on the host.

Copy the example environment file and edit the domain and basic-auth hash:

```bash
cp deployments/.env.example deployments/.env
caddy hash-password --plaintext 'choose-a-strong-password'
```

Paste the generated hash into `PANEL_BASIC_AUTH_HASH`. Keep the plaintext password somewhere safe; the agent can use it with `--api-basic-pass` when it talks to the panel through Caddy.

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

## 4. Back up SQLite

For a conservative backup, briefly stop the panel before copying the database file out of the named Docker volume:

```bash
mkdir -p backups
docker compose -f deployments/docker-compose.yml stop panel
docker run --rm -v sing-panel_panel-data:/data -v "$PWD/backups:/backups" alpine:3.22 sh -c 'cp /data/sing-panel.db /backups/sing-panel-$(date +%Y%m%d-%H%M%S).db'
docker compose -f deployments/docker-compose.yml start panel
```

Restore by stopping the panel and copying a chosen backup back to `/data/sing-panel.db` in the same volume.

## 5. Real traffic statistics check

After the panel is reachable, deploy an Exit node with a `sing-box` binary built with `with_v2ray_api` and `with_acme`, set the Exit node `stats_mode` to `v2ray-api`, and point `stats_api_listen` at the sing-box API listener, for example `127.0.0.1:10085`.

If the Exit node uses ACME DNS01 with Cloudflare, set the environment variable named in the node's Cloudflare token field on the agent host:

```bash
export CLOUDFLARE_API_TOKEN='...'
```

The panel stores only the environment variable name. The agent resolves `api_token_env` to sing-box's real `api_token` field before writing `sing-box.json`.

Run the agent on the Exit host with:

```bash
go run ./apps/agent --api-url https://panel.example.com --node-id <exit-node-id> --stats-mode auto --check-config --sing-box-bin /usr/local/bin/sing-box --api-basic-user admin --api-basic-pass 'choose-a-strong-password'
```

The agent will fetch desired config, validate it with `sing-box check`, and upload per-user traffic deltas from V2Ray API stats.

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
