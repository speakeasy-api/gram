#!/bin/bash
set -euo pipefail

GRAM_SERVER_URL="${GRAM_SERVER_URL:?GRAM_SERVER_URL required}"
GRAM_API_KEY="${GRAM_API_KEY:?GRAM_API_KEY required}"
GRAM_PROJECT_SLUG="${GRAM_PROJECT_SLUG:?GRAM_PROJECT_SLUG required}"

SANDBOX_HOME="/home/sandbox"
OPENCLAW_HOME="${SANDBOX_HOME}/.openclaw"
POLICY_REGO="/etc/defenseclaw/openshell-policy.rego"
POLICY_DATA="/etc/defenseclaw/openshell-policy.yaml"

echo "============================================"
echo "DefenseClaw x Gram — Provisioning (OpenShell)"
echo "============================================"

# ---------------------------------------------------------------
# 1. Fetch MCP servers from Gram and write OpenClaw config
# ---------------------------------------------------------------
echo "[1/5] Fetching MCP servers from Gram (project: ${GRAM_PROJECT_SLUG})..."

python3 -c "
import json, os
from urllib.request import Request, urlopen
from pathlib import Path

server_url = os.environ['GRAM_SERVER_URL'].rstrip('/')
api_key = os.environ['GRAM_API_KEY']
project_slug = os.environ['GRAM_PROJECT_SLUG']

req = Request(f'{server_url}/rpc/toolsets.list')
req.add_header('Gram-Key', api_key)
req.add_header('Gram-Project', project_slug)

with urlopen(req) as resp:
    data = json.loads(resp.read())

mcp_servers = {}
for ts in data.get('toolsets', []):
    if not ts.get('mcp_enabled') or not ts.get('mcp_slug'):
        continue
    name = ts.get('slug', ts.get('name', 'unknown'))
    url = f'{server_url}/mcp/{ts[\"mcp_slug\"]}'
    mcp_servers[name] = {
        'command': 'npx',
        'args': ['-y', 'mcp-remote', url, '--header', f'Gram-Key:{api_key}'],
    }
    print(f'  {name}: {url} (via mcp-remote)')

config = {
    'mcp': {'servers': mcp_servers},
    'gateway': {
        'mode': 'local',
        'port': 18789,
        'bind': 'lan',
        'controlUi': {'dangerouslyAllowHostHeaderOriginFallback': True},
        'auth': {'token': 'poc-token'},
    },
    'models': {
        'providers': {
            'gram': {
                'baseUrl': server_url,
                'apiKey': 'unused',
                'authHeader': False,
                'headers': {
                    'Gram-Key': api_key,
                    'Gram-Project': project_slug,
                },
                'api': 'openai-completions',
                'models': [{'id': 'anthropic/claude-sonnet-4.5', 'name': 'Claude Sonnet 4.5'}],
            }
        }
    },
    'agents': {
        'defaults': {
            'model': {'primary': 'gram/anthropic/claude-sonnet-4.5'}
        }
    },
}

config_dir = Path('${OPENCLAW_HOME}')
config_dir.mkdir(parents=True, exist_ok=True)
(config_dir / 'openclaw.json').write_text(json.dumps(config, indent=2))
print(f'  Configured {len(mcp_servers)} MCP server(s)')
"

# ---------------------------------------------------------------
# 2. Generate OpenShell network policy YAML
# ---------------------------------------------------------------
echo "[2/5] Generating OpenShell network policy..."

GRAM_HOST=$(python3 -c "import os; print(os.environ['GRAM_SERVER_URL'].rstrip('/').split('://')[-1].split(':')[0])")

cat > "${POLICY_DATA}" <<YAML
# OpenShell network policy for DefenseClaw x Gram POC
# Generated at container start — only these endpoints are reachable.
# All other connections are denied by OPA policy per-CONNECT.

network_policies:
  allow_defenseclaw_sidecar:
    binaries:
    - path: /**
    endpoints:
    - host: "10.200.0.1"
      ports: [18970, 4000]
      tls: skip

  allow_gram:
    binaries:
    - path: /**
    endpoints:
    - host: "${GRAM_HOST}"
      ports: [443]
      tls: skip

  allow_npm_registry:
    binaries:
    - path: /**
    endpoints:
    - host: "registry.npmjs.org"
      ports: [443]
      tls: skip

  allow_pypi:
    binaries:
    - path: /**
    endpoints:
    - host: "pypi.org"
      ports: [443]
      tls: skip
    - host: "files.pythonhosted.org"
      ports: [443]
      tls: skip

  allow_example:
    binaries:
    - path: /**
    endpoints:
    - host: "example.com"
      ports: [80, 443]
      tls: skip
YAML

echo "  Policy: ${POLICY_DATA}"
echo "    allow_gram: ${GRAM_HOST}:443"
echo "    allow_npm_registry: registry.npmjs.org:443"
echo "    allow_example: example.com:80,443"
echo "    default: DENY (all other connections blocked by OPA)"

# ---------------------------------------------------------------
# 3. Prepare sandbox environment
# ---------------------------------------------------------------
echo "[3/5] Preparing sandbox environment..."

# Copy start-openclaw.sh to sandbox home
cp /poc/start-openclaw.sh "${SANDBOX_HOME}/start-openclaw.sh"
chmod +x "${SANDBOX_HOME}/start-openclaw.sh"

# Symlink so root user's `openclaw agent --local` can find the config
ln -sf "${OPENCLAW_HOME}" /root/.openclaw

# Write sandbox resolv.conf (DNS via public resolver, forwarded through host)
cat > /etc/defenseclaw/sandbox-resolv.conf <<EOF
nameserver 8.8.8.8
nameserver 8.8.4.4
EOF

# Fix ownership
chown -R sandbox:sandbox "${SANDBOX_HOME}"

echo "  Sandbox user: sandbox"
echo "  OpenClaw home: ${OPENCLAW_HOME}"
echo "  DNS: 8.8.8.8 (forwarded through host-side veth)"

# ---------------------------------------------------------------
# 4. Start OpenShell sandbox
# ---------------------------------------------------------------
echo "[4/5] Starting OpenShell sandbox..."

export OPENCLAW_GATEWAY_TOKEN="poc-token"

# openshell-sandbox creates:
#   - Network namespace with veth pair (10.200.0.1 ↔ 10.200.0.2)
#   - HTTP CONNECT proxy on 10.200.0.1:3128 (evaluates OPA Rego policy)
#   - Landlock LSM filesystem restrictions
#   - seccomp-BPF syscall filters
# Then drops privileges to sandbox user and execs start-openclaw.sh.
#
# unshare --mount isolates the resolv.conf bind-mount so it only
# affects the sandbox, not the container's own DNS.
unshare --mount -- bash -c "
    mount --bind /etc/defenseclaw/sandbox-resolv.conf /etc/resolv.conf
    exec openshell-sandbox \
        --policy-rules ${POLICY_REGO} \
        --policy-data ${POLICY_DATA} \
        --log-level info \
        --timeout 0 \
        -w ${SANDBOX_HOME} \
        -- ${SANDBOX_HOME}/start-openclaw.sh
" &

SANDBOX_PID=$!
echo "  OpenShell sandbox started (pid ${SANDBOX_PID})"

# ---------------------------------------------------------------
# 5. Post-sandbox setup (wait for veth, iptables forwarding)
# ---------------------------------------------------------------
echo "[5/5] Waiting for sandbox network..."

# Wait for veth pair to come up (created by openshell-sandbox)
VETH_READY=false
for i in $(seq 1 30); do
    if ip link show 2>/dev/null | grep -q "veth-h"; then
        VETH_READY=true
        VETH_NAME=$(ip link show | grep "veth-h" | head -1 | awk '{print $2}' | cut -d@ -f1)
        echo "  veth pair ready: ${VETH_NAME}"
        break
    fi
    sleep 1
done

if ! ${VETH_READY}; then
    echo "  WARN: veth pair did not appear in 30s"
fi

# Enable IP forwarding
sysctl -qw net.ipv4.ip_forward=1
sysctl -qw net.ipv4.conf.all.route_localnet=1

# --- Inject iptables INSIDE the sandbox namespace ---
# Find the sandbox init process (child of openshell-sandbox)
sleep 1
SANDBOX_NS_PID=""
for pid in $(pgrep -P "${SANDBOX_PID}" 2>/dev/null) ${SANDBOX_PID}; do
    if [ -d "/proc/${pid}/ns" ]; then
        SANDBOX_NS_PID="${pid}"
        break
    fi
done

if [ -n "${SANDBOX_NS_PID}" ]; then
    echo "  Injecting iptables rules inside sandbox namespace (pid ${SANDBOX_NS_PID})..."

    # Allow traffic to the host-side veth (proxy, sidecar, guardrail)
    nsenter --target "${SANDBOX_NS_PID}" --net -- iptables -I OUTPUT -d 10.200.0.1 -p tcp --dport 3128 -j ACCEPT 2>/dev/null || true
    nsenter --target "${SANDBOX_NS_PID}" --net -- iptables -I OUTPUT -d 10.200.0.1 -p tcp --dport 18970 -j ACCEPT 2>/dev/null || true
    nsenter --target "${SANDBOX_NS_PID}" --net -- iptables -I OUTPUT -d 10.200.0.1 -p tcp --dport 4000 -j ACCEPT 2>/dev/null || true

    # Allow DNS
    nsenter --target "${SANDBOX_NS_PID}" --net -- iptables -I OUTPUT -p udp --dport 53 -j ACCEPT 2>/dev/null || true
    nsenter --target "${SANDBOX_NS_PID}" --net -- iptables -I OUTPUT -p tcp --dport 53 -j ACCEPT 2>/dev/null || true
else
    echo "  WARN: could not find sandbox namespace PID — skipping iptables injection"
fi

# --- Host-side iptables ---

# DNS forwarding: sandbox → upstream (8.8.8.8)
iptables -A FORWARD -s 10.200.0.0/24 -p udp --dport 53 -j ACCEPT
iptables -A FORWARD -d 10.200.0.0/24 -p udp --sport 53 -j ACCEPT
iptables -t nat -A POSTROUTING -s 10.200.0.0/24 -p udp --dport 53 -j MASQUERADE

# Gateway port forwarding: *:18789 → sandbox:18789
iptables -t nat -A OUTPUT -d 127.0.0.1 -p tcp --dport 18789 -j DNAT --to-destination 10.200.0.2:18789
iptables -t nat -A PREROUTING -p tcp --dport 18789 -j DNAT --to-destination 10.200.0.2:18789
iptables -t nat -A POSTROUTING -d 10.200.0.2 -p tcp --dport 18789 -j MASQUERADE
iptables -A FORWARD -d 10.200.0.2 -p tcp --dport 18789 -j ACCEPT
iptables -A FORWARD -s 10.200.0.2 -p tcp --sport 18789 -j ACCEPT

echo "  DNS forwarding: sandbox → 8.8.8.8 (via MASQUERADE)"
echo "  Gateway forwarding: *:18789 → 10.200.0.2:18789"

# Wait for gateway health
echo "  Waiting for OpenClaw gateway..."
for i in $(seq 1 30); do
    if curl -sf http://10.200.0.2:18789/healthz 2>/dev/null | grep -qi "ok\|healthy"; then
        echo "  Gateway is ready (inside sandbox at 10.200.0.2:18789)"
        break
    fi
    sleep 2
done

# ---------------------------------------------------------------
# Print connection info
# ---------------------------------------------------------------
echo
echo "============================================"
echo "DefenseClaw x Gram — RUNNING (OpenShell)"
echo "============================================"
echo
echo "Sandbox:         OpenShell (pid ${SANDBOX_PID})"
echo "  Network:       10.200.0.2 (isolated namespace)"
echo "  Proxy:         10.200.0.1:3128 (OPA policy enforced)"
echo "  Filesystem:    Landlock restricted"
echo "  Syscalls:      seccomp-BPF filtered"
echo
echo "OpenClaw:        ws://10.200.0.2:18789 (inside sandbox)"
echo "  Forwarded to:  ws://localhost:18789"
echo "MCP servers:     from Gram project '${GRAM_PROJECT_SLUG}'"
echo "Completions:     ${GRAM_SERVER_URL}/chat/completions (via proxy)"
echo
echo "Network policy:  ${POLICY_DATA}"
echo "  Allowed:       ${GRAM_HOST}, registry.npmjs.org, example.com"
echo "  Blocked:       everything else (asdf.com, api.openai.com, ...)"
echo
echo "--- Send a message: ---"
echo
echo "  docker exec -e OPENCLAW_GATEWAY_TOKEN=poc-token $(hostname) \\"
echo "    openclaw agent --local -m 'What tools do you have available?'"
echo
echo "============================================"
echo

# Keep container alive — follow sandbox process
wait ${SANDBOX_PID}
