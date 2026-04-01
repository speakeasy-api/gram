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

exec openclaw gateway run \
    --port 18789 \
    --bind lan \
    --auth token \
    --token "${OPENCLAW_GATEWAY_TOKEN:-poc-token}"
