#!/bin/bash
set -euo pipefail
#
# Runs INSIDE the OpenShell sandbox (10.200.0.2).
#
# All HTTP/HTTPS traffic is routed through OpenShell's HTTP CONNECT proxy
# on the host side of the veth pair. The proxy evaluates the OPA network
# policy per-connection — only endpoints in the policy YAML are reachable.
#

# openshell-sandbox drops privileges but does not set HOME
export HOME=/home/sandbox

# Route all traffic through OpenShell's CONNECT proxy (OPA policy enforced)
export HTTPS_PROXY=http://10.200.0.1:3128
export HTTP_PROXY=http://10.200.0.1:3128
export NO_PROXY=10.200.0.1,localhost,127.0.0.1

# Start gateway in background, run network policy tests once healthy, then wait
openclaw gateway run \
    --port 18789 \
    --bind lan \
    --auth token \
    --token "${OPENCLAW_GATEWAY_TOKEN:-poc-token}" &

GW_PID=$!

# Wait for gateway to start, then run network policy smoke tests.
# We wait on the PID's /proc entry + a delay rather than curl, because
# the sandbox's transparent proxy intercepts all TCP (even to localhost).
sleep 5
if kill -0 $GW_PID 2>/dev/null; then
    # These tests run INSIDE the sandbox, so their process ancestry passes
    # OpenShell's OPA binary integrity checks (no /.fly/hallpass in tree).
    python3 /poc/test-network-policy.py || true
fi

wait $GW_PID
