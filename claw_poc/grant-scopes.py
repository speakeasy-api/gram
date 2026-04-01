#!/usr/bin/env python3
"""Grant full operator scopes to all paired devices.

Waits for paired.json to be created by the OpenClaw gateway (happens on
first client connection), then updates all devices with full scopes.
"""
import json
import time
from pathlib import Path

paired = Path("/home/sandbox/.openclaw/devices/paired.json")

# Wait for paired.json to appear
for _ in range(15):
    if paired.exists():
        break
    time.sleep(2)

if not paired.exists():
    print("No paired.json — skipping")
    exit(0)

data = json.loads(paired.read_text())
scopes = [
    "operator.read",
    "operator.write",
    "operator.admin",
    "operator.approvals",
    "operator.pairing",
]
for dev in data.values():
    dev["scopes"] = scopes
    dev["approvedScopes"] = scopes
    for t in dev.get("tokens", {}).values():
        t["scopes"] = scopes

paired.write_text(json.dumps(data, indent=2))
print("Device scopes updated")
