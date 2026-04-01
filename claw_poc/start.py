#!/usr/bin/env python3
"""
DefenseClaw x Gram — Container entrypoint.

Provisions an OpenClaw instance inside an OpenShell sandbox, pre-configured
with a Gram tenant's MCP servers and network policy.
"""
import json
import os
import shutil
import signal
import subprocess
import sys
import time
from pathlib import Path
from urllib.request import Request, urlopen

# ---------------------------------------------------------------------------
# Required env vars
# ---------------------------------------------------------------------------
GRAM_SERVER_URL = os.environ.get("GRAM_SERVER_URL") or sys.exit("GRAM_SERVER_URL required")
GRAM_API_KEY = os.environ.get("GRAM_API_KEY") or sys.exit("GRAM_API_KEY required")
GRAM_PROJECT_SLUG = os.environ.get("GRAM_PROJECT_SLUG") or sys.exit("GRAM_PROJECT_SLUG required")

GRAM_SERVER_URL = GRAM_SERVER_URL.rstrip("/")
GRAM_HOST = GRAM_SERVER_URL.split("://")[-1].split(":")[0]

SANDBOX_HOME = Path("/home/sandbox")
OPENCLAW_HOME = SANDBOX_HOME / ".openclaw"
POLICY_REGO = Path("/etc/defenseclaw/openshell-policy.rego")
POLICY_DATA = Path("/etc/defenseclaw/openshell-policy.yaml")
SANDBOX_RESOLV = Path("/etc/defenseclaw/sandbox-resolv.conf")


def run(cmd: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, shell=True, check=check, capture_output=True, text=True)


def log(msg: str) -> None:
    print(msg, flush=True)


# ---------------------------------------------------------------
# 1. Fetch MCP servers from Gram and write OpenClaw config
# ---------------------------------------------------------------
log("============================================")
log("DefenseClaw x Gram — Provisioning (OpenShell)")
log("============================================")
log(f"[1/5] Fetching MCP servers from Gram (project: {GRAM_PROJECT_SLUG})...")

req = Request(f"{GRAM_SERVER_URL}/rpc/toolsets.list")
req.add_header("Gram-Key", GRAM_API_KEY)
req.add_header("Gram-Project", GRAM_PROJECT_SLUG)

with urlopen(req) as resp:
    data = json.loads(resp.read())

mcp_servers: dict = {}
for ts in data.get("toolsets", []):
    if not ts.get("mcp_enabled") or not ts.get("mcp_slug"):
        continue
    name = ts.get("slug", ts.get("name", "unknown"))
    url = f'{GRAM_SERVER_URL}/mcp/{ts["mcp_slug"]}'
    mcp_servers[name] = {
        "command": "npx",
        "args": ["-y", "mcp-remote", url, "--header", f"Gram-Key:{GRAM_API_KEY}"],
    }
    log(f"  {name}: {url} (via mcp-remote)")

openclaw_config = {
    "mcp": {"servers": mcp_servers},
    "gateway": {
        "mode": "local",
        "port": 18789,
        "bind": "lan",
        "controlUi": {"dangerouslyAllowHostHeaderOriginFallback": True},
        "auth": {"token": "poc-token"},
    },
    "models": {
        "providers": {
            "gram": {
                "baseUrl": GRAM_SERVER_URL,
                "apiKey": "unused",
                "authHeader": False,
                "headers": {
                    "Gram-Key": GRAM_API_KEY,
                    "Gram-Project": GRAM_PROJECT_SLUG,
                },
                "api": "openai-completions",
                "models": [{"id": "anthropic/claude-sonnet-4.5", "name": "Claude Sonnet 4.5"}],
            }
        }
    },
    "agents": {
        "defaults": {
            "model": {"primary": "gram/anthropic/claude-sonnet-4.5"}
        }
    },
}

OPENCLAW_HOME.mkdir(parents=True, exist_ok=True)
(OPENCLAW_HOME / "openclaw.json").write_text(json.dumps(openclaw_config, indent=2))
log(f"  Configured {len(mcp_servers)} MCP server(s)")

# ---------------------------------------------------------------
# 2. Generate OpenShell network policy YAML
# ---------------------------------------------------------------
log(f"[2/5] Generating OpenShell network policy...")

POLICY_DATA.parent.mkdir(parents=True, exist_ok=True)
POLICY_DATA.write_text(f"""\
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
    - host: "{GRAM_HOST}"
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
""")

log(f"  Policy: {POLICY_DATA}")
log(f"    allow_gram: {GRAM_HOST}:443")
log(f"    allow_npm_registry: registry.npmjs.org:443")
log(f"    allow_example: example.com:80,443")
log(f"    default: DENY (all other connections blocked by OPA)")

# ---------------------------------------------------------------
# 3. Prepare sandbox environment
# ---------------------------------------------------------------
log("[3/5] Preparing sandbox environment...")

# Copy start-openclaw.sh to sandbox home
shutil.copy2("/poc/start-openclaw.sh", SANDBOX_HOME / "start-openclaw.sh")
(SANDBOX_HOME / "start-openclaw.sh").chmod(0o755)

# Symlink so root user's `openclaw agent --local` can find the config
root_openclaw = Path("/root/.openclaw")
if root_openclaw.exists() or root_openclaw.is_symlink():
    root_openclaw.unlink()
root_openclaw.symlink_to(OPENCLAW_HOME)

# Write sandbox resolv.conf (DNS via public resolver, forwarded through host)
SANDBOX_RESOLV.write_text("nameserver 8.8.8.8\nnameserver 8.8.4.4\n")

# --- Security: lock down policy files (mirrors DefenseClaw architecture) ---
# Policy files are owned by root and immutable from inside the sandbox.
# The openshell-sandbox binary loads them at startup on the host side —
# the sandbox process cannot modify them to weaken its own restrictions.
# Even if the sandbox user overwrote the YAML on disk, the running proxy
# would not reload it (policy is loaded once at startup).
for policy_file in [POLICY_REGO, POLICY_DATA, SANDBOX_RESOLV]:
    os.chown(policy_file, 0, 0)       # root:root
    os.chmod(policy_file, 0o644)       # readable by all, writable only by root
os.chmod(POLICY_DATA.parent, 0o755)    # /etc/defenseclaw/ traversable but not writable

# Grant sandbox user ownership of its home (OpenClaw config, skills, workspace)
# but NOT the policy directory
run(f"chown -R sandbox:sandbox {SANDBOX_HOME}")

log(f"  Sandbox user: sandbox")
log(f"  OpenClaw home: {OPENCLAW_HOME} (owned by sandbox)")
log(f"  Policy dir: {POLICY_DATA.parent} (owned by root, read-only to sandbox)")
log(f"  DNS: 8.8.8.8 (forwarded through host-side veth)")

# ---------------------------------------------------------------
# 4. Start OpenShell sandbox
# ---------------------------------------------------------------
log("[4/5] Starting OpenShell sandbox...")

os.environ["OPENCLAW_GATEWAY_TOKEN"] = "poc-token"

# openshell-sandbox creates:
#   - Network namespace with veth pair (10.200.0.1 <-> 10.200.0.2)
#   - HTTP CONNECT proxy on 10.200.0.1:3128 (evaluates OPA Rego policy)
#   - Landlock LSM filesystem restrictions
#   - seccomp-BPF syscall filters
# Then drops privileges to sandbox user and execs start-openclaw.sh.
#
# unshare --mount isolates the resolv.conf bind-mount so it only
# affects the sandbox, not the container's own DNS.
sandbox_proc = subprocess.Popen(
    [
        "unshare", "--mount", "--", "bash", "-c",
        f"mount --bind {SANDBOX_RESOLV} /etc/resolv.conf && "
        f"exec openshell-sandbox "
        f"--policy-rules {POLICY_REGO} "
        f"--policy-data {POLICY_DATA} "
        f"--log-level info "
        f"--timeout 0 "
        f"-w {SANDBOX_HOME} "
        f"-- {SANDBOX_HOME}/start-openclaw.sh",
    ],
)

log(f"  OpenShell sandbox started (pid {sandbox_proc.pid})")

# ---------------------------------------------------------------
# 5. Post-sandbox setup (wait for veth, iptables forwarding)
# ---------------------------------------------------------------
log("[5/5] Waiting for sandbox network...")

# Wait for veth pair to come up (created by openshell-sandbox)
veth_name = None
for _ in range(30):
    result = run("ip link show", check=False)
    if result.stdout and "veth-h" in result.stdout:
        for line in result.stdout.splitlines():
            if "veth-h" in line:
                veth_name = line.split()[1].split("@")[0]
                break
        break
    time.sleep(1)

if veth_name:
    log(f"  veth pair ready: {veth_name}")
else:
    log("  WARN: veth pair did not appear in 30s")

# Enable IP forwarding
run("sysctl -qw net.ipv4.ip_forward=1")
run("sysctl -qw net.ipv4.conf.all.route_localnet=1")

# --- Inject iptables INSIDE the sandbox namespace ---
sandbox_ns_pid = None
result = run(f"pgrep -P {sandbox_proc.pid}", check=False)
candidates = (result.stdout.split() if result.stdout else []) + [str(sandbox_proc.pid)]
for pid in candidates:
    if Path(f"/proc/{pid}/ns").is_dir():
        sandbox_ns_pid = pid
        break

if sandbox_ns_pid:
    log(f"  Injecting iptables rules inside sandbox namespace (pid {sandbox_ns_pid})...")
    for port in [3128, 18970, 4000]:
        run(f"nsenter --target {sandbox_ns_pid} --net -- iptables -I OUTPUT -d 10.200.0.1 -p tcp --dport {port} -j ACCEPT", check=False)
    for proto in ["udp", "tcp"]:
        run(f"nsenter --target {sandbox_ns_pid} --net -- iptables -I OUTPUT -p {proto} --dport 53 -j ACCEPT", check=False)
else:
    log("  WARN: could not find sandbox namespace PID — skipping iptables injection")

# --- Host-side iptables ---

# DNS forwarding: sandbox -> upstream (8.8.8.8)
run("iptables -A FORWARD -s 10.200.0.0/24 -p udp --dport 53 -j ACCEPT")
run("iptables -A FORWARD -d 10.200.0.0/24 -p udp --sport 53 -j ACCEPT")
run("iptables -t nat -A POSTROUTING -s 10.200.0.0/24 -p udp --dport 53 -j MASQUERADE")

# Gateway port forwarding: *:18789 -> sandbox:18789
run("iptables -t nat -A OUTPUT -d 127.0.0.1 -p tcp --dport 18789 -j DNAT --to-destination 10.200.0.2:18789")
run("iptables -t nat -A PREROUTING -p tcp --dport 18789 -j DNAT --to-destination 10.200.0.2:18789")
run("iptables -t nat -A POSTROUTING -d 10.200.0.2 -p tcp --dport 18789 -j MASQUERADE")
run("iptables -A FORWARD -d 10.200.0.2 -p tcp --dport 18789 -j ACCEPT")
run("iptables -A FORWARD -s 10.200.0.2 -p tcp --sport 18789 -j ACCEPT")

log(f"  DNS forwarding: sandbox -> 8.8.8.8 (via MASQUERADE)")
log(f"  Gateway forwarding: *:18789 -> 10.200.0.2:18789")

# Wait for gateway health
log("  Waiting for OpenClaw gateway...")
gateway_ready = False
for _ in range(30):
    result = run("curl -sf http://10.200.0.2:18789/healthz", check=False)
    if result.returncode == 0 and result.stdout and any(w in result.stdout.lower() for w in ["ok", "healthy"]):
        log("  Gateway is ready (inside sandbox at 10.200.0.2:18789)")
        gateway_ready = True
        break
    time.sleep(2)

if not gateway_ready:
    log("  WARN: Gateway did not become healthy after 60s")

# ---------------------------------------------------------------
# Print connection info
# ---------------------------------------------------------------
hostname = os.environ.get("HOSTNAME", "dc-poc")
log("")
log("============================================")
log("DefenseClaw x Gram — RUNNING (OpenShell)")
log("============================================")
log("")
log(f"Sandbox:         OpenShell (pid {sandbox_proc.pid})")
log(f"  Network:       10.200.0.2 (isolated namespace)")
log(f"  Proxy:         10.200.0.1:3128 (OPA policy enforced)")
log(f"  Filesystem:    Landlock restricted")
log(f"  Syscalls:      seccomp-BPF filtered")
log("")
log(f"OpenClaw:        ws://10.200.0.2:18789 (inside sandbox)")
log(f"  Forwarded to:  ws://localhost:18789")
log(f"MCP servers:     from Gram project '{GRAM_PROJECT_SLUG}'")
log(f"Completions:     {GRAM_SERVER_URL}/chat/completions (via proxy)")
log("")
log(f"Network policy:  {POLICY_DATA}")
log(f"  Allowed:       {GRAM_HOST}, registry.npmjs.org, example.com")
log(f"  Blocked:       everything else (asdf.com, api.openai.com, ...)")
log("")
log("--- Send a message: ---")
log("")
log(f"  docker exec -e OPENCLAW_GATEWAY_TOKEN=poc-token {hostname} \\")
log(f"    openclaw agent --local -m 'What tools do you have available?'")
log("")
log("============================================")
log("")

# Forward SIGTERM to sandbox process for clean shutdown
def _forward_signal(signum, _frame):
    sandbox_proc.send_signal(signum)

signal.signal(signal.SIGTERM, _forward_signal)
signal.signal(signal.SIGINT, _forward_signal)

# Keep container alive — follow sandbox process
sys.exit(sandbox_proc.wait())
