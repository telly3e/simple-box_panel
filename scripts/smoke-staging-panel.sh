#!/usr/bin/env bash
set -euo pipefail

PANEL_URL=""
BASIC_USER=""
BASIC_PASS=""
NODE_HOST="staging-smoke.example.com"
SKIP_CLEANUP=0

usage() {
  cat <<'EOF'
Usage:
  scripts/smoke-staging-panel.sh --panel-url URL [options]

Options:
  --panel-url URL       Panel base URL, for example http://127.0.0.1:8080
  --basic-user USER     Optional HTTP Basic Auth username
  --basic-pass PASS     Optional HTTP Basic Auth password
  --node-host HOST      Temporary smoke node hostname (default: staging-smoke.example.com)
  --skip-cleanup        Keep temporary smoke records for inspection
  -h, --help            Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --panel-url)
      PANEL_URL="${2:-}"
      shift 2
      ;;
    --basic-user)
      BASIC_USER="${2:-}"
      shift 2
      ;;
    --basic-pass)
      BASIC_PASS="${2:-}"
      shift 2
      ;;
    --node-host)
      NODE_HOST="${2:-}"
      shift 2
      ;;
    --skip-cleanup)
      SKIP_CLEANUP=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$PANEL_URL" ]]; then
  echo "--panel-url is required" >&2
  usage >&2
  exit 2
fi

PANEL_URL="${PANEL_URL%/}"
CREATED_USER_ID=""
CREATED_NODE_ID=""

CURL_AUTH=()
if [[ -n "$BASIC_USER" || -n "$BASIC_PASS" ]]; then
  CURL_AUTH=(-u "${BASIC_USER}:${BASIC_PASS}")
fi

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

json_get() {
  local expr="$1"
  python3 -c 'import json,sys; data=json.load(sys.stdin); print(eval(sys.argv[1], {}, {"data": data}))' "$expr"
}

json_assert() {
  local expr="$1"
  local message="$2"
  shift 2
  python3 -c 'import json,sys; data=json.load(sys.stdin); ok=bool(eval(sys.argv[1], {}, {"data": data, "args": sys.argv[2:]})); sys.exit(0 if ok else 1)' "$expr" "$@" || {
    echo "$message" >&2
    exit 1
  }
}

api() {
  local method="$1"
  local path="$2"
  shift 2
  local body=""
  if [[ $# -gt 0 ]]; then
    body="$1"
    shift
  fi
  local extra_headers=("$@")
  local args=(-fsS -X "$method" "${CURL_AUTH[@]}" -H "Content-Type: application/json")
  local header
  for header in "${extra_headers[@]}"; do
    args+=(-H "$header")
  done
  if [[ -n "$body" ]]; then
    args+=(--data "$body")
  fi
  curl "${args[@]}" "${PANEL_URL}${path}"
}

cleanup() {
  if [[ "$SKIP_CLEANUP" -eq 1 ]]; then
    return
  fi
  if [[ -n "$CREATED_USER_ID" ]]; then
    echo "Cleaning up temporary user..."
    curl -fsS -X DELETE "${CURL_AUTH[@]}" "${PANEL_URL}/api/users/${CREATED_USER_ID}" >/dev/null || true
  fi
  if [[ -n "$CREATED_NODE_ID" ]]; then
    echo "Cleaning up temporary node..."
    curl -fsS -X DELETE "${CURL_AUTH[@]}" "${PANEL_URL}/api/exit-nodes/${CREATED_NODE_ID}" >/dev/null || true
  fi
}
trap cleanup EXIT

need_cmd curl
need_cmd python3

echo "Checking panel health..."
health="$(api GET /api/health)"
printf '%s' "$health" | json_assert 'data.get("status") == "ok"' "health check did not return ok"

stamp="$(date +%Y%m%d-%H%M%S)"
padding=$'stop=2\n0=10-20\n1=30-40'

echo "Creating temporary smoke user..."
user_body="$(python3 -c 'import json,sys; print(json.dumps({"name": sys.argv[1], "quota_bytes": 0}, separators=(",", ":")))' "staging-smoke-${stamp}")"
user="$(api POST /api/users "$user_body")"
CREATED_USER_ID="$(printf '%s' "$user" | json_get 'data["id"]')"
subscription_token="$(printf '%s' "$user" | json_get 'data["subscription_token"]')"
printf '%s' "$user" | json_assert 'bool(data.get("id")) and bool(data.get("subscription_token")) and bool(data.get("ss_2022_password_32"))' "user was not initialized"

echo "Creating temporary smoke node..."
node_body="$(python3 -c '
import json,sys
print(json.dumps({
  "name": sys.argv[1],
  "hostname": sys.argv[2],
  "enabled": True,
  "anytls_enabled": True,
  "ss_enabled": True,
  "anytls_port": 2443,
  "anytls_padding_scheme": sys.argv[3],
  "ss_port": 8388,
  "ss_method": "2022-blake3-chacha20-poly1305",
  "cert_mode": "manual",
  "certificate_path": "/etc/sing-box/smoke-cert.pem",
  "key_path": "/etc/sing-box/smoke-key.pem",
  "stats_mode": "mock"
}, separators=(",", ":")))
' "Staging Smoke ${stamp}" "$NODE_HOST" "$padding")"
node="$(api POST /api/exit-nodes "$node_body")"
CREATED_NODE_ID="$(printf '%s' "$node" | json_get 'data["id"]')"
agent_token="$(printf '%s' "$node" | json_get 'data["agent_token"]')"
initial_version="$(printf '%s' "$node" | json_get 'data["expected_config_version"]')"
printf '%s' "$node" | json_assert 'bool(data.get("id")) and bool(data.get("agent_token"))' "node was not initialized"

echo "Checking generated subscription..."
subscription="$(api GET "/sub/${subscription_token}")"
printf '%s' "$subscription" | json_assert 'any(o.get("type") == "shadowsocks" and o.get("method") == "2022-blake3-chacha20-poly1305" and ":" in o.get("password", "") and o.get("multiplex", {}).get("enabled") is True for o in data.get("outbounds", []))' "subscription Shadowsocks outbound was not generated as expected"

echo "Checking desired config..."
desired="$(api GET "/api/agent/${CREATED_NODE_ID}/desired-config" "" "X-Sing-Panel-Agent-Token: ${agent_token}")"
printf '%s' "$desired" | json_assert 'any(i.get("type") == "anytls" and i.get("padding_scheme") == ["stop=2","0=10-20","1=30-40"] for i in data.get("sing_box_config", {}).get("inbounds", []))' "desired config did not use custom AnyTLS padding"
printf '%s' "$desired" | json_assert 'any(i.get("type") == "shadowsocks" and i.get("method") == "2022-blake3-chacha20-poly1305" and i.get("multiplex", {}).get("enabled") is True for i in data.get("sing_box_config", {}).get("inbounds", []))' "desired config Shadowsocks inbound was not generated as expected"

echo "Checking patch/version flow..."
patch_body="$(python3 -c 'import json; print(json.dumps({"anytls_padding_scheme": "stop=1\n0=20-30", "ss_method": "aes-256-gcm"}, separators=(",", ":")))')"
patched="$(api PATCH "/api/exit-nodes/${CREATED_NODE_ID}" "$patch_body")"
patched_version="$(printf '%s' "$patched" | json_get 'data["expected_config_version"]')"
if [[ "$patched_version" -le "$initial_version" ]]; then
  echo "patch did not bump desired config version" >&2
  exit 1
fi
agent_token="$(printf '%s' "$patched" | json_get 'data["agent_token"]')"
desired_after_patch="$(api GET "/api/agent/${CREATED_NODE_ID}/desired-config" "" "X-Sing-Panel-Agent-Token: ${agent_token}")"
printf '%s' "$desired_after_patch" | json_assert 'any(i.get("type") == "shadowsocks" and i.get("method") == "aes-256-gcm" for i in data.get("sing_box_config", {}).get("inbounds", []))' "patched desired config did not switch SS method"
desired_version="$(printf '%s' "$desired_after_patch" | json_get 'data["version"]')"

echo "Checking heartbeat/applied status..."
heartbeat_body="$(python3 -c 'import json,sys; print(json.dumps({"applied_config_version": int(sys.argv[1])}, separators=(",", ":")))' "$desired_version")"
api POST "/api/agent/${CREATED_NODE_ID}/heartbeat" "$heartbeat_body" "X-Sing-Panel-Agent-Token: ${agent_token}" >/dev/null
nodes="$(api GET /api/exit-nodes)"
printf '%s' "$nodes" | json_assert 'any(n.get("id") == args[1] and n.get("applied_config_version") == int(args[0]) for n in data)' "heartbeat did not update applied config version" "$desired_version" "$CREATED_NODE_ID"

echo "Checking pause flow..."
paused="$(api PATCH "/api/exit-nodes/${CREATED_NODE_ID}" '{"enabled":false}')"
agent_token="$(printf '%s' "$paused" | json_get 'data["agent_token"]')"
paused_desired="$(api GET "/api/agent/${CREATED_NODE_ID}/desired-config" "" "X-Sing-Panel-Agent-Token: ${agent_token}")"
printf '%s' "$paused_desired" | json_assert 'data.get("paused") is True and len(data.get("sing_box_config", {}).get("inbounds", [])) == 0' "paused desired config was not empty"
paused_version="$(printf '%s' "$paused_desired" | json_get 'data["version"]')"

echo "Staging panel smoke passed."
python3 -c 'import json,sys; print(json.dumps({"ok": True, "panel": sys.argv[1], "smoke_user_id": sys.argv[2], "smoke_node_id": sys.argv[3], "desired_version": int(sys.argv[4]), "cleanup": sys.argv[5] == "0"}, separators=(",", ":")))' \
  "$PANEL_URL" "$CREATED_USER_ID" "$CREATED_NODE_ID" "$paused_version" "$SKIP_CLEANUP"
