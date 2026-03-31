#!/bin/bash
set -euo pipefail

GRAM_SERVER_URL="${GRAM_SERVER_URL:?GRAM_SERVER_URL required}"
GRAM_API_KEY="${GRAM_API_KEY:?GRAM_API_KEY required}"
GRAM_PROJECT_SLUG="${GRAM_PROJECT_SLUG:?GRAM_PROJECT_SLUG required}"

echo "============================================"
echo "DefenseClaw x Gram — Provisioning"
echo "============================================"

# ---------------------------------------------------------------
# 1. Fetch MCP servers from Gram and write openclaw.json
# ---------------------------------------------------------------
echo "[1/3] Fetching MCP servers from Gram (project: ${GRAM_PROJECT_SLUG})..."

python3 -c "
import json, os, sys
from urllib.request import Request, urlopen

server_url = os.environ['GRAM_SERVER_URL'].rstrip('/')
api_key = os.environ['GRAM_API_KEY']
project_slug = os.environ['GRAM_PROJECT_SLUG']
from pathlib import Path

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
    # Use mcp-remote as stdio-to-HTTP bridge (OpenClaw npm build only supports stdio)
    mcp_servers[name] = {
        'command': 'npx',
        'args': ['-y', 'mcp-remote', url, '--header', f'Gram-Key:{api_key}'],
    }
    print(f'  {name}: {url} (via mcp-remote)')

config = {'mcp': {'servers': mcp_servers}}
config_dir = Path.home() / '.openclaw'
config_dir.mkdir(parents=True, exist_ok=True)
(config_dir / 'openclaw.json').write_text(json.dumps(config, indent=2))
print(f'  Configured {len(mcp_servers)} MCP server(s)')
"

# ---------------------------------------------------------------
# 2. Apply network policy (iptables egress firewall)
# ---------------------------------------------------------------
echo "[2/4] Applying network policy..."

python3 -c "
import json, os, subprocess, socket

# Allowed hosts from the policy (hardcoded to match config.py)
allowed_hosts = [
    'app.getgram.ai',
    'registry.npmjs.org',
    'pypi.org',
    'files.pythonhosted.org',
    'example.com',
]

# Also allow DNS and loopback
rules = []
rules.append('iptables -P OUTPUT DROP')
rules.append('iptables -A OUTPUT -o lo -j ACCEPT')
rules.append('iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT')
# Allow DNS so we can resolve hostnames
rules.append('iptables -A OUTPUT -p udp --dport 53 -j ACCEPT')
rules.append('iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT')

for host in allowed_hosts:
    try:
        ips = set()
        for info in socket.getaddrinfo(host, None, socket.AF_INET):
            ips.add(info[4][0])
        for ip in ips:
            rules.append(f'iptables -A OUTPUT -d {ip} -j ACCEPT')
            print(f'  Allow {host} -> {ip}')
    except socket.gaierror:
        print(f'  Skip {host} (DNS failed)')

for rule in rules:
    subprocess.run(rule, shell=True, check=True)

print('  Default policy: DROP (all other egress blocked)')
"

# ---------------------------------------------------------------
# 3. Start OpenClaw gateway in background
# ---------------------------------------------------------------
echo "[3/4] Starting OpenClaw gateway..."

# Configure gateway mode + bind
python3 -c "
import json
from pathlib import Path
config_path = Path.home() / '.openclaw' / 'openclaw.json'
config = json.loads(config_path.read_text())
config.setdefault('gateway', {})['mode'] = 'local'
config['gateway'].setdefault('controlUi', {})['dangerouslyAllowHostHeaderOriginFallback'] = True
config['gateway'].setdefault('auth', {})['token'] = 'poc-token'

# Configure Gram as LLM provider (OpenAI-compatible)
import os
server_url = os.environ['GRAM_SERVER_URL']
api_key = os.environ['GRAM_API_KEY']
config.setdefault('models', {}).setdefault('providers', {})['gram'] = {
    'baseUrl': server_url,
    'apiKey': 'unused',
    'authHeader': False,
    'headers': {
        'Gram-Key': api_key,
        'Gram-Project': os.environ['GRAM_PROJECT_SLUG'],
    },
    'api': 'openai-completions',
    'models': [{'id': 'anthropic/claude-sonnet-4.5', 'name': 'Claude Sonnet 4.5'}],
}
config.setdefault('agents', {}).setdefault('defaults', {}).setdefault('model', {})['primary'] = 'gram/anthropic/claude-sonnet-4.5'
config_path.write_text(json.dumps(config, indent=2))
"

export OPENCLAW_GATEWAY_TOKEN="poc-token"

openclaw gateway run \
  --port 18789 \
  --auth token \
  --token "${OPENCLAW_GATEWAY_TOKEN}" \
  &

GATEWAY_PID=$!

# Wait for gateway to be ready
echo "  Waiting for gateway (pid ${GATEWAY_PID})..."
for i in $(seq 1 30); do
    if openclaw health 2>/dev/null | grep -q "ok\|healthy\|running"; then
        echo "  Gateway is ready"
        break
    fi
    sleep 1
done

# ---------------------------------------------------------------
# 4. Print connection info
# ---------------------------------------------------------------
echo
echo "============================================"
echo "DefenseClaw x Gram — RUNNING"
echo "============================================"
echo
echo "OpenClaw gateway:  ws://localhost:18789"
echo "MCP servers:       from Gram project '${GRAM_PROJECT_SLUG}'"
echo "Completions:       ${GRAM_SERVER_URL}/chat/completions"
echo
echo "--- Send a message from your host: ---"
echo
echo "  docker exec -it \$(docker ps -q --filter ancestor=defenseclaw-gram:poc) \\"
echo "    openclaw agent --local -m 'What tools do you have available?'"
echo
echo "--- Or interactive shell: ---"
echo
echo "  docker exec -it \$(docker ps -q --filter ancestor=defenseclaw-gram:poc) bash"
echo "  openclaw agent --local -m 'Use the pizza-map tool with pepperoni'"
echo
echo "============================================"
echo

# Keep container alive — follow gateway process
wait ${GATEWAY_PID}
