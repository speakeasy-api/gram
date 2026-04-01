#!/usr/bin/env python3
"""Test OpenShell network policy enforcement from inside the sandbox.

This script runs inside the sandbox (started by start-openclaw.sh or
invoked by the gateway). Because it inherits the sandbox's process tree,
its ancestor chain passes OpenShell's OPA binary integrity checks.

Usage (from inside sandbox):
    python3 /poc/test-network-policy.py

Prints PASS/FAIL lines that poc.py parses from the Fly logs.
"""
import os
import subprocess
import sys

os.environ.setdefault("HTTPS_PROXY", "http://10.200.0.1:3128")
os.environ.setdefault("HTTP_PROXY", "http://10.200.0.1:3128")

tests = [
    ("allow", "https://example.com", "Example"),
    ("deny", "https://asdf.com", None),
]

for action, url, expect in tests:
    result = subprocess.run(
        ["curl", "-s", "--max-time", "10", url],
        capture_output=True, text=True,
    )
    if action == "allow":
        if result.returncode == 0 and expect and expect in result.stdout:
            print(f"NETWORK_TEST PASS: {url} reachable (allowed by OPA policy)")
        else:
            print(f"NETWORK_TEST FAIL: {url} not reachable (exit={result.returncode})")
    elif action == "deny":
        if result.returncode != 0:
            print(f"NETWORK_TEST PASS: {url} blocked (denied by OPA policy)")
        else:
            print(f"NETWORK_TEST FAIL: {url} was NOT blocked")
