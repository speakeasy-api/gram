#!/usr/bin/env python3
"""
DefenseClaw x Gram POC — End-to-End (Fly.io)

Tears down any existing Fly app, deploys a fresh one, waits for the
gateway to be ready, then runs tests to verify:
  - MCP tool calls via Gram
  - OpenShell network policy enforcement (allow/deny)

Usage:
    fly auth login          # one-time
    uv run python poc.py
"""
import subprocess
import sys
import time

from config import GRAM_SERVER_URL, GRAM_PROJECT_SLUG, GRAM_API_KEY

APP_NAME = "defenseclaw-gram-poc"


def run(cmd, timeout=300, check=True):
    """Run a shell command, return stdout."""
    print(f"  $ {cmd}")
    result = subprocess.run(
        cmd, shell=True, capture_output=True, text=True, timeout=timeout,
    )
    if check and result.returncode != 0:
        stderr = result.stderr.strip() if result.stderr else ""
        raise RuntimeError(f"Command failed ({result.returncode}): {stderr}")
    return result.stdout.strip() if result.stdout else ""


def fly_ssh(cmd, timeout=120):
    """Run a command on the Fly machine via SSH."""
    return run(
        f"fly ssh console -a {APP_NAME} -C '{cmd}'",
        timeout=timeout,
    )


def fly_ssh_ok(cmd, timeout=30):
    """Run a command on the Fly machine, return (success, output)."""
    full = f"fly ssh console -a {APP_NAME} -C '{cmd}'"
    print(f"  $ {cmd}")
    result = subprocess.run(
        full, shell=True, capture_output=True, text=True, timeout=timeout + 10,
    )
    return result.returncode == 0, result.stdout or result.stderr or ""


# =========================================================================
# Step 1: Tear down existing app
# =========================================================================
print("=" * 60)
print("Step 1: Tear down existing Fly app")
print("=" * 60)

run(f"fly apps destroy {APP_NAME} --yes", check=False)
print("  Done")

# =========================================================================
# Step 2: Create app and set secrets
# =========================================================================
print()
print("=" * 60)
print("Step 2: Create Fly app and set secrets")
print("=" * 60)

run(f"fly apps create {APP_NAME} --org personal")
run(
    f"fly secrets set -a {APP_NAME} "
    f"GRAM_API_KEY={GRAM_API_KEY} "
    f"GRAM_PROJECT_SLUG={GRAM_PROJECT_SLUG} "
    f"GRAM_SERVER_URL={GRAM_SERVER_URL}"
)
print("  Secrets set")

# =========================================================================
# Step 3: Deploy
# =========================================================================
print()
print("=" * 60)
print("Step 3: Deploy to Fly.io")
print("=" * 60)

output = run(f"fly deploy -a {APP_NAME} --wait-timeout 120", timeout=600)
for line in output.splitlines()[-5:]:
    print(f"  {line}")

# =========================================================================
# Step 4: Wait for gateway
# =========================================================================
print()
print("=" * 60)
print("Step 4: Wait for OpenClaw gateway (inside OpenShell sandbox)")
print("=" * 60)

ready = False
logs = ""
for i in range(45):
    logs = run(f"fly logs -a {APP_NAME} --no-tail 2>&1 | tail -50", check=False)
    if "Gateway is ready" in logs or "listening on ws://" in logs:
        ready = True
        for line in logs.splitlines():
            if any(k in line for k in [
                "Fetching", "Configured", "listening", "agent model",
                "via mcp-remote", "allow_", "DENY", "veth pair",
                "OpenShell", "Sandbox", "Gateway is ready", "Proxy:",
            ]):
                print(f"  {line.split('] ')[-1] if '] ' in line else line}")
        break
    sys.stdout.write(".")
    sys.stdout.flush()
    time.sleep(2)

if not ready:
    print("\n  FAIL: Gateway did not start in 90s")
    print("  Logs:")
    print(logs)
    sys.exit(1)

print("  Gateway is ready")

# Wait for paired.json then grant full scopes
for attempt in range(15):
    time.sleep(2)
    ok, _ = fly_ssh_ok("test -f /home/sandbox/.openclaw/devices/paired.json")
    if ok:
        break
else:
    print("  WARN: paired.json not found after 30s — device pairing may fail")

fly_ssh("""python3 -c "
import json
from pathlib import Path
paired_path = Path('/home/sandbox/.openclaw/devices/paired.json')
if not paired_path.exists():
    print('No paired.json yet — skipping scope update')
    exit(0)
data = json.loads(paired_path.read_text())
full_scopes = ['operator.read', 'operator.write', 'operator.admin', 'operator.approvals', 'operator.pairing']
for device in data.values():
    device['scopes'] = full_scopes
    device['approvedScopes'] = full_scopes
    for t in device.get('tokens', {}).values():
        t['scopes'] = full_scopes
paired_path.write_text(json.dumps(data, indent=2))
print('Device scopes updated')
" """)

# =========================================================================
# Step 5: Send a prompt (MCP tool call via Gram)
# =========================================================================
print()
print("=" * 60)
print("Step 5: Send prompt to OpenClaw agent (MCP tool call)")
print("=" * 60)

print()
print('  Prompt: "Use the pizza-map tool with topping pepperoni and tell me the result"')
print()

response = fly_ssh(
    'OPENCLAW_GATEWAY_TOKEN=poc-token openclaw agent --local --session-id poc-test '
    '-m "Use the pizza-map tool with topping pepperoni and tell me the result"',
    timeout=120,
)

for line in response.splitlines():
    if line.startswith("[") and ("]" in line[:40]):
        continue
    if line.strip():
        print(f"  {line}")

# =========================================================================
# Step 6: Test network policy — allowed host (example.com)
# =========================================================================
print()
print("=" * 60)
print("Step 6: curl example.com from sandbox (ALLOWED in OpenShell policy)")
print("=" * 60)

print()

# curl from inside the sandbox network namespace via the OpenShell proxy
ok, output = fly_ssh_ok(
    'bash -c \'GW_PID=$(pgrep -f openclaw-gateway | head -1); '
    'nsenter --target $GW_PID --net -- '
    'curl -sf --proxy http://10.200.0.1:3128 --max-time 15 https://example.com\''
)

if ok and "Example" in output:
    print("  PASS: example.com reachable (allowed by OPA policy)")
else:
    print(f"  FAIL: example.com not reachable (ok={ok})")
    if output:
        print(f"  output: {output.strip()[:200]}")

# =========================================================================
# Step 7: Test network policy — blocked host (asdf.com)
# =========================================================================
print()
print("=" * 60)
print("Step 7: curl asdf.com from sandbox (NOT in policy — should be blocked)")
print("=" * 60)

print()

ok, output = fly_ssh_ok(
    'bash -c \'GW_PID=$(pgrep -f openclaw-gateway | head -1); '
    'nsenter --target $GW_PID --net -- '
    'curl -sf --proxy http://10.200.0.1:3128 --max-time 15 https://asdf.com\''
)

if not ok:
    print("  PASS: asdf.com blocked by OpenShell proxy (connection denied by OPA policy)")
else:
    print("  FAIL: asdf.com was NOT blocked — network policy not enforced")
    print(f"  Got: {output[:200]}")

# Check proxy deny log
deny_log = run(f"fly logs -a {APP_NAME} --no-tail 2>&1 | grep 'CONNECT.*asdf.com'", check=False)
if deny_log and "deny" in deny_log:
    print(f"  Proxy log: DENIED")

# =========================================================================
# Summary
# =========================================================================
print()
print("=" * 60)
print("POC Complete")
print("=" * 60)
print()
print(f"  App:          {APP_NAME}")
print(f"  Runtime:      Fly.io Firecracker VM")
print(f"  Sandbox:      OpenShell (namespace + Landlock + seccomp)")
print(f"  Network:      OPA/Rego policy via HTTP CONNECT proxy")
print(f"  LLM provider: Gram ({GRAM_SERVER_URL}/chat/completions)")
print(f"  MCP servers:  from Gram project '{GRAM_PROJECT_SLUG}'")
print()
print("  Send more prompts:")
print(f"    fly ssh console -a {APP_NAME} -C \\")
print(f'      \'OPENCLAW_GATEWAY_TOKEN=poc-token openclaw agent --local -m "your message"\'')
print()
print(f"  View logs:")
print(f"    fly logs -a {APP_NAME}")
print()
print(f"  Tear down:")
print(f"    fly apps destroy {APP_NAME} --yes")
