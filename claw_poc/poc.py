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
import json
import subprocess
import sys
import time

from config import GRAM_SERVER_URL, GRAM_PROJECT_SLUG, GRAM_API_KEY

APP_NAME = "defenseclaw-gram-poc"
FLY_ORG = "speakeasy-lab"


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


MACHINE_ID = None  # set after deploy, used to pin all SSH commands to one machine


def _fly_ssh_args(cmd):
    args = ["fly", "ssh", "console", "-a", APP_NAME]
    if MACHINE_ID:
        args += ["--machine", MACHINE_ID]
    args += ["-C", cmd]
    return args


def fly_ssh(cmd, timeout=120, check=True):
    """Run a command on the Fly machine via SSH.

    If check=True (default), raises on failure and returns stdout.
    If check=False, returns (success: bool, output: str).
    """
    print(f"  $ {cmd[:120]}{'...' if len(cmd) > 120 else ''}")
    result = subprocess.run(
        _fly_ssh_args(cmd),
        capture_output=True, text=True, timeout=timeout,
    )
    if check:
        if result.returncode != 0:
            stderr = result.stderr.strip() if result.stderr else ""
            raise RuntimeError(f"Command failed ({result.returncode}): {stderr}")
        return result.stdout.strip() if result.stdout else ""
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

run(f"fly apps create {APP_NAME} --org {FLY_ORG}")
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

# Pin all SSH commands to the running machine
machine_list = run(f"fly machines list -a {APP_NAME} --json", check=False)
if machine_list:
    machines = json.loads(machine_list)
    started = [m for m in machines if m.get("state") == "started"]
    if started:
        MACHINE_ID = started[0]["id"]
        print(f"  Machine: {MACHINE_ID}")

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
    result = subprocess.run(
        f"fly logs -a {APP_NAME} --no-tail 2>&1 | tail -50",
        shell=True, capture_output=True, text=True, timeout=30,
    )
    logs = result.stdout.strip() if result.stdout else ""
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

# Trigger device auto-pairing by connecting, then grant full scopes.
# The grant-scopes.py script is baked into the image to avoid shell quoting issues.
fly_ssh("bash -c 'OPENCLAW_GATEWAY_TOKEN=poc-token openclaw health 2>/dev/null || true'", timeout=30)
fly_ssh("python3 /poc/grant-scopes.py", timeout=60)

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
    "bash -c 'OPENCLAW_GATEWAY_TOKEN=poc-token openclaw agent --local --session-id poc-test "
    '-m "Use the pizza-map tool with topping pepperoni and tell me the result"\'',
    timeout=120,
)

for line in response.splitlines():
    if line.startswith("[") and ("]" in line[:40]):
        continue
    if line.strip():
        print(f"  {line}")

# =========================================================================
# Step 6: Network policy tests (run inside sandbox, check logs)
# =========================================================================
print()
print("=" * 60)
print("Step 6: Network policy tests (run inside sandbox at startup)")
print("=" * 60)

print()
print("  Tests run inside the sandbox process tree so OPA binary checks pass.")
print("  Checking Fly logs for NETWORK_TEST results...")
print()

# Wait for test results to appear in logs
network_results = ""
for _ in range(15):
    result = subprocess.run(
        f"fly logs -a {APP_NAME} --no-tail 2>&1 | grep NETWORK_TEST",
        shell=True, capture_output=True, text=True, timeout=30,
    )
    network_results = result.stdout.strip() if result.stdout else ""
    if network_results:
        break
    time.sleep(2)

if network_results:
    for line in network_results.splitlines():
        # Strip ANSI codes and fly log prefixes
        clean = line.split("NETWORK_TEST ")[-1] if "NETWORK_TEST " in line else line
        print(f"  {clean}")
else:
    print("  WARN: No network test results found in logs")

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
