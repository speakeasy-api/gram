#!/usr/bin/env python3
"""
DefenseClaw x Gram POC — End-to-End

Tears down any existing POC container, builds and starts a fresh one,
waits for the gateway to be ready, then sends a prompt through OpenClaw
to verify the full stack works: Gram completions + MCP tool calls.

The container runs OpenClaw inside an OpenShell sandbox with:
  - Network namespace isolation (veth pair: 10.200.0.1 ↔ 10.200.0.2)
  - HTTP CONNECT proxy with OPA/Rego network policy enforcement
  - Landlock LSM filesystem restrictions
  - seccomp-BPF syscall filtering

Usage:
    uv run python poc.py
"""
import json
import subprocess
import sys
import time

from config import GRAM_SERVER_URL, GRAM_PROJECT_SLUG, GRAM_API_KEY

CONTAINER_NAME = "dc-poc"
IMAGE_NAME = "defenseclaw-gram:poc"


def run(cmd, timeout=120, check=True, capture=True):
    """Run a shell command, return stdout."""
    print(f"  $ {cmd}")
    result = subprocess.run(
        cmd, shell=True, capture_output=capture, text=True, timeout=timeout,
    )
    if check and result.returncode != 0:
        stderr = result.stderr.strip() if result.stderr else ""
        raise RuntimeError(f"Command failed ({result.returncode}): {stderr}")
    return result.stdout.strip() if result.stdout else ""


def docker_exec(cmd, timeout=120):
    """Run a command inside the POC container."""
    return run(
        f"docker exec -e OPENCLAW_GATEWAY_TOKEN=poc-token {CONTAINER_NAME} {cmd}",
        timeout=timeout,
    )


# =========================================================================
# Step 1: Tear down existing container
# =========================================================================
print("=" * 60)
print("Step 1: Tear down existing container")
print("=" * 60)

run(f"docker rm -f {CONTAINER_NAME}", check=False)
print("  Done")

# =========================================================================
# Step 2: Build image
# =========================================================================
print()
print("=" * 60)
print("Step 2: Build Docker image")
print("=" * 60)

output = run(f"docker build -t {IMAGE_NAME} .", timeout=600)
# Show last few lines of build output
for line in output.splitlines()[-3:]:
    print(f"  {line}")

# =========================================================================
# Step 3: Start container
# =========================================================================
print()
print("=" * 60)
print("Step 3: Start container (privileged — OpenShell needs namespaces)")
print("=" * 60)

# --privileged is required because openshell-sandbox creates:
#   - Linux namespaces (PID, network, mount, IPC, UTS) via unshare
#   - veth pair for network isolation
#   - Landlock LSM policies
#   - seccomp-BPF filters
# These kernel operations require elevated capabilities inside Docker.
run(
    f"docker run -d --name {CONTAINER_NAME} "
    f"--privileged "
    f"-p 18789:18789 "
    f"-e GRAM_API_KEY='{GRAM_API_KEY}' "
    f"-e GRAM_PROJECT_SLUG='{GRAM_PROJECT_SLUG}' "
    f"-e GRAM_SERVER_URL='{GRAM_SERVER_URL}' "
    f"{IMAGE_NAME}"
)

# =========================================================================
# Step 4: Wait for gateway
# =========================================================================
print()
print("=" * 60)
print("Step 4: Wait for OpenClaw gateway (inside OpenShell sandbox)")
print("=" * 60)

ready = False
for i in range(45):
    time.sleep(2)
    logs = run(f"docker logs {CONTAINER_NAME}", check=False)
    if "Gateway is ready" in logs or "listening on ws://" in logs:
        ready = True
        # Print the provisioning summary from logs
        for line in logs.splitlines():
            if any(k in line for k in [
                "Fetching", "Configured", "listening", "agent model",
                "via mcp-remote", "allow_", "DENY", "veth pair",
                "OpenShell", "Sandbox", "Gateway is ready", "Landlock",
                "seccomp", "Proxy:", "forwarding",
            ]):
                print(f"  {line.split('] ')[-1] if '] ' in line else line}")
        break
    sys.stdout.write(".")
    sys.stdout.flush()

if not ready:
    print("\n  FAIL: Gateway did not start in 90s")
    print("  Logs:")
    print(run(f"docker logs {CONTAINER_NAME}", check=False))
    sys.exit(1)

print("  Gateway is ready")

# Wait for paired.json to be created by the gateway, then grant full scopes
for attempt in range(15):
    time.sleep(2)
    result = subprocess.run(
        f"docker exec {CONTAINER_NAME} test -f /home/sandbox/.openclaw/devices/paired.json",
        shell=True, capture_output=True,
    )
    if result.returncode == 0:
        break
else:
    print("  WARN: paired.json not found after 30s — device pairing may fail")

docker_exec("""python3 -c "
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
# Step 5: Send a prompt
# =========================================================================
print()
print("=" * 60)
print("Step 5: Send prompt to OpenClaw agent")
print("=" * 60)

print()
print('  Prompt: "Use the pizza-map tool with topping pepperoni and tell me the result"')
print()

response = docker_exec(
    "openclaw agent --local --session-id poc-test "
    '-m "Use the pizza-map tool with topping pepperoni and tell me the result"',
    timeout=120,
)

# Filter out noise lines
for line in response.splitlines():
    if line.startswith("[") and ("]" in line[:40]):
        continue  # skip log noise like [bundle-mcp], [agents/model-providers]
    if line.strip():
        print(f"  {line}")

# =========================================================================
# Step 6: Test network policy — allowed host (example.com)
# =========================================================================
print()
print("=" * 60)
print("Step 6: Fetch example.com (ALLOWED in OpenShell policy)")
print("=" * 60)

print()
print('  Prompt: "Fetch http://example.com and tell me the page title"')
print()

response = docker_exec(
    "openclaw agent --local --session-id poc-fetch-allowed "
    '-m "Fetch http://example.com using web_fetch and tell me the page title. Be brief."',
    timeout=120,
)

example_ok = False
for line in response.splitlines():
    if line.startswith("[") and ("]" in line[:40]):
        continue
    if line.strip():
        print(f"  {line}")
        if "example" in line.lower():
            example_ok = True

if example_ok:
    print("\n  PASS: example.com reachable (in OpenShell allow list)")
else:
    print("\n  WARN: could not confirm example.com content")

# =========================================================================
# Step 7: Test network policy — blocked host (asdf.com)
# =========================================================================
print()
print("=" * 60)
print("Step 7: Fetch asdf.com (NOT in OpenShell policy — should be blocked)")
print("=" * 60)

print()
print('  Prompt: "Fetch http://asdf.com and tell me what you get"')
print()

response = docker_exec(
    "openclaw agent --local --session-id poc-fetch-blocked "
    '-m "Fetch http://asdf.com using web_fetch and tell me exactly what happened. Be brief."',
    timeout=120,
)

blocked = False
for line in response.splitlines():
    if line.startswith("[") and ("]" in line[:40]):
        continue
    if line.strip():
        print(f"  {line}")
        if any(w in line.lower() for w in ["error", "fail", "block", "refused", "timeout", "unreachable", "unable", "couldn't", "cannot", "denied", "reject"]):
            blocked = True

if blocked:
    print("\n  PASS: asdf.com blocked (not in OpenShell policy)")
else:
    print("\n  FAIL: asdf.com was NOT blocked — network policy not enforced")

# =========================================================================
# Summary
# =========================================================================
print()
print("=" * 60)
print("POC Complete")
print("=" * 60)
print()
print(f"  Container:    {CONTAINER_NAME}")
print(f"  Image:        {IMAGE_NAME}")
print(f"  Sandbox:      OpenShell (namespace + Landlock + seccomp)")
print(f"  Network:      OPA/Rego policy via HTTP CONNECT proxy")
print(f"  Gateway:      ws://localhost:18789 (forwarded to 10.200.0.2)")
print(f"  LLM provider: Gram ({GRAM_SERVER_URL}/chat/completions)")
print(f"  MCP servers:  from Gram project '{GRAM_PROJECT_SLUG}'")
print()
print("  Send more prompts:")
print(f"    docker exec -e OPENCLAW_GATEWAY_TOKEN=poc-token {CONTAINER_NAME} \\")
print(f'      openclaw agent --local --session-id mytest -m "your message"')
print()
print("  Interactive shell:")
print(f"    docker exec -it {CONTAINER_NAME} bash")
print()
print(f"  Tear down:")
print(f"    docker rm -f {CONTAINER_NAME}")
