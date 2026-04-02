# DefenseClaw x Gram — Minimal POC

## Goal

Provision a single OpenClaw instance governed by DefenseClaw, integrated with Gram. Demonstrate all four requirements:

1. OpenClaw sandboxed with OpenShell
2. All chat completions forwarded to Gram
3. DefenseClaw guardrails installed by default
4. MCP servers preconfigured for a given Gram org

## Approach

Single Docker image + provisioning entrypoint. No Gram server changes, no DB tables, no Temporal workflows, no Fly.io provisioning. Just `docker run` with env vars.

The container fetches MCP servers from Gram's API at boot time using a `producer+consumer` scoped API key, then configures OpenClaw with those servers.

## What Already Works

- `defenseclaw init --sandbox` → OpenShell setup, sandbox user, policies
- `defenseclaw setup guardrail` → patches openclaw.json, starts guardrail proxy
- Guardrail proxy (`internal/gateway/proxy.go`) → intercepts all LLM calls
- OpenShell → network namespace + iptables + Landlock + seccomp

## Env Vars (set by Gram at provision time)

```bash
GRAM_API_KEY=gram_live_abc123... # API key with "producer" scope (includes consumer implicitly)
GRAM_PROJECT_SLUG=my-project     # which project to pull toolsets from
GRAM_SERVER_URL=https://gram.example.com
GRAM_POLICY=default              # default|strict|permissive
```

Create the API key in the Gram dashboard (org settings → API Keys → scope: producer). The container derives everything else from the Gram API.

## Deliverables

### 1. Dockerfile

```dockerfile
FROM python:3.12-slim AS base
RUN apt-get update && apt-get install -y curl iptables iproute2 sudo nodejs npm

# OpenClaw
RUN npm install -g @openclaw/cli

# DefenseClaw Go binary
COPY --from=builder /defenseclaw-gateway /usr/local/bin/

# DefenseClaw Python CLI + scanners
RUN pip install defenseclaw skill-scanner mcp-scanner

# OpenShell sandbox binary (from NVIDIA OCI registry)
COPY scripts/install-openshell-sandbox.sh /tmp/
RUN bash /tmp/install-openshell-sandbox.sh

# Provisioning scripts
COPY poc/entrypoint.sh /entrypoint.sh
COPY poc/fetch-and-configure.py /usr/local/lib/defenseclaw/fetch-and-configure.py
COPY poc/generate-policy.py /usr/local/lib/defenseclaw/generate-policy.py

ENTRYPOINT ["/entrypoint.sh"]
```

### 2. `poc/entrypoint.sh` — provisioning entrypoint

```bash
#!/bin/bash
set -euo pipefail

# 1. Fetch toolsets from Gram API → write openclaw.json + export MCP URLs
python3 /usr/local/lib/defenseclaw/fetch-and-configure.py
# Outputs: /tmp/mcp-endpoints.json (used by policy generator)

# 2. Init DefenseClaw with sandbox
defenseclaw init --sandbox --non-interactive

# 3. Setup guardrail proxy pointing upstream at Gram's completions endpoint
defenseclaw setup guardrail \
  --upstream "$GRAM_SERVER_URL/rpc/completions" \
  --api-key "$GRAM_API_KEY" \
  --non-interactive

# 4. Generate OpenShell policy allowing ONLY Gram endpoints
python3 /usr/local/lib/defenseclaw/generate-policy.py

# 5. Start sandbox (OpenClaw launches inside it)
systemctl start defenseclaw-sandbox.target

# Keep container alive
exec tail -f /var/log/defenseclaw/*.log
```

### 3. `poc/fetch-and-configure.py` — fetch toolsets from Gram API, write openclaw.json

```python
"""
Fetch MCP-enabled toolsets from Gram API and generate openclaw.json.

Uses GRAM_API_KEY (producer scope) to list toolsets, then builds the
MCP server config that OpenClaw reads at startup. The same API key
(consumer scope) is passed as auth header for runtime MCP calls.
"""
import json
import os
import sys
from pathlib import Path
from urllib.request import Request, urlopen

server_url = os.environ["GRAM_SERVER_URL"].rstrip("/")
api_key = os.environ["GRAM_API_KEY"]
project_slug = os.environ["GRAM_PROJECT_SLUG"]

# Fetch toolsets (requires producer scope)
req = Request(f"{server_url}/rpc/toolsets.list")
req.add_header("Gram-Key", api_key)
req.add_header("Gram-Project", project_slug)

with urlopen(req) as resp:
    data = json.loads(resp.read())

# Filter to MCP-enabled toolsets and build openclaw.json
mcp_servers = {}
mcp_endpoints = []

for ts in data.get("toolsets", []):
    if not ts.get("mcp_enabled"):
        continue
    mcp_slug = ts.get("mcp_slug")
    if not mcp_slug:
        continue

    url = f"{server_url}/mcp/{mcp_slug}"
    mcp_servers[ts["slug"]] = {
        "url": url,
        "transport": "streamable-http",
        "headers": {"Gram-Key": api_key},
    }
    mcp_endpoints.append({"name": ts["slug"], "url": url})

if not mcp_servers:
    print("WARNING: no MCP-enabled toolsets found for this project", file=sys.stderr)

# Write openclaw.json
config = {"mcp": {"servers": mcp_servers}}
config_dir = Path.home() / ".openclaw"
config_dir.mkdir(parents=True, exist_ok=True)
(config_dir / "openclaw.json").write_text(json.dumps(config, indent=2))

# Write endpoints list for policy generator
Path("/tmp/mcp-endpoints.json").write_text(json.dumps(mcp_endpoints, indent=2))

print(f"Configured {len(mcp_servers)} MCP server(s) from Gram project '{project_slug}'")
```

### 4. `poc/generate-policy.py` — generate OpenShell network policy

```python
"""
Generate OpenShell Rego policy data that allows ONLY:
- Gram MCP server endpoints (fetched in step 1)
- Gram completions endpoint (for guardrail proxy upstream)
- DefenseClaw sidecar (host-side veth)
"""
import json
import os
from pathlib import Path
from urllib.parse import urlparse

server_url = os.environ["GRAM_SERVER_URL"]
mcp_endpoints = json.loads(Path("/tmp/mcp-endpoints.json").read_text())

# Collect allowed endpoints
allowed = []

# Gram server (covers MCP URLs + completions endpoint)
parsed = urlparse(server_url)
port = parsed.port or (443 if parsed.scheme == "https" else 80)
allowed.append({"host": parsed.hostname, "port": port})

# Any MCP servers on different hosts (e.g. custom domains)
for ep in mcp_endpoints:
    parsed = urlparse(ep["url"])
    p = parsed.port or (443 if parsed.scheme == "https" else 80)
    allowed.append({"host": parsed.hostname, "port": p})

# DefenseClaw sidecar on host side of veth
allowed.append({"host": "10.200.0.1", "port": 4000})   # guardrail proxy
allowed.append({"host": "10.200.0.1", "port": 18970})   # sidecar API

# De-duplicate
seen = set()
unique = []
for e in allowed:
    key = (e["host"], e["port"])
    if key not in seen:
        seen.add(key)
        unique.append(e)

# Write Rego data file (consumed by default.rego)
policy_data = {
    "network_policies": {
        f"allow_{e['host'].replace('.', '_')}_{e['port']}": {
            "endpoints": [{"host": e["host"], "ports": [e["port"]]}]
        }
        for e in unique
    }
}

policy_dir = Path.home() / ".defenseclaw"
policy_dir.mkdir(parents=True, exist_ok=True)
(policy_dir / "openshell-policy.yaml").write_text(json.dumps(policy_data, indent=2))

print(f"OpenShell policy: allowing {len(unique)} endpoint(s), blocking all other egress")
```

## One Change to DefenseClaw

`defenseclaw setup guardrail` needs an `--upstream` flag so the guardrail proxy forwards LLM calls to Gram's completions endpoint instead of directly to the LLM provider.

**File:** `cli/defenseclaw/guardrail.py`

Currently `patch_openclaw_config()` (line ~50) sets `baseUrl: http://localhost:4000` pointing at the local proxy, and the proxy forwards to the configured LLM provider. The change: when `--upstream` is provided, the proxy's upstream target becomes that URL instead of the LLM provider's API.

This is the only code change needed. Everything else is orchestration of existing commands.

## Demo

```bash
docker run --privileged \
  -e GRAM_API_KEY='gram_key_abc123' \
  -e GRAM_PROJECT_SLUG='my-project' \
  -e GRAM_SERVER_URL='https://gram.example.com' \
  -e GRAM_POLICY='default' \
  defenseclaw-gram:poc
```

`--privileged` required for OpenShell (iptables, network namespaces, Landlock).

## Testing the Requirements

### 1. OpenClaw sandboxed with OpenShell

```bash
# Verify sandbox running
defenseclaw sandbox status
# Expected: openshell-sandbox.service active

# Verify egress blocked
defenseclaw sandbox exec -- curl -s --max-time 3 https://evil.com
# Expected: timeout / connection refused

# Verify allowed endpoint works
defenseclaw sandbox exec -- curl -s https://gram.example.com/health
# Expected: 200
```

### 2. All chat completions forwarded to Gram

```bash
# Verify guardrail proxy is up
curl -s http://localhost:4000/health
# Expected: 200

# Verify openclaw.json routes LLM calls through proxy
cat ~/.openclaw/openclaw.json | python3 -c "
import json, sys
c = json.load(sys.stdin)
provider = c['models']['providers']['defenseclaw']
assert 'localhost:4000' in provider['baseUrl'], 'proxy not configured'
print('OK: LLM calls routed through guardrail proxy')
"

# Verify direct LLM access blocked from sandbox
defenseclaw sandbox exec -- curl -s --max-time 3 https://api.anthropic.com
# Expected: timeout (blocked by OpenShell)
```

### 3. DefenseClaw guardrails installed by default

```bash
# Verify guardrail proxy running
curl -s http://localhost:4000/health
# Expected: 200

# Verify plugin installed
ls ~/.openclaw/extensions/defenseclaw/
# Expected: dist/ directory with plugin files
```

### 4. MCP servers preconfigured for the given Gram org

```bash
# Verify MCP servers in config match Gram project's toolsets
cat ~/.openclaw/openclaw.json | python3 -c "
import json, sys
c = json.load(sys.stdin)
servers = c.get('mcp', {}).get('servers', {})
print(f'{len(servers)} MCP server(s) configured:')
for name, cfg in servers.items():
    print(f'  {name}: {cfg[\"url\"]}')
"

# Verify agent can reach MCP servers
defenseclaw sandbox exec -- openclaw mcp list
# Expected: lists tools from Gram-hosted MCP servers
```

### Automated smoke test

```bash
docker run --privileged \
  -e GRAM_API_KEY='gram_key_test' \
  -e GRAM_PROJECT_SLUG='test-project' \
  -e GRAM_SERVER_URL='https://gram.example.com' \
  -e GRAM_POLICY='default' \
  defenseclaw-gram:poc \
  /poc/smoke-test.sh
```

## What This Proves

- OpenClaw boots inside OpenShell sandbox with network isolation
- Agent can only reach Gram's endpoints (all other egress blocked by iptables)
- All LLM calls flow through guardrail proxy → Gram's completions endpoint
- MCP servers fetched from Gram API at boot (not hardcoded)
- Agent can execute arbitrary code but network jail prevents exfiltration

## What the POC Does NOT Include

- Gram server changes (no new API endpoints, DB tables, or Temporal workflows)
- Fly.io provisioning (manual `docker run` for now)
- Telemetry pipeline to Gram's ClickHouse (log to stdout, wire later)
- Dashboard UI
- Multi-instance management
