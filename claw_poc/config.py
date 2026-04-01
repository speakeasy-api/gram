"""
DefenseClaw x Gram — Configuration

The inputs needed to provision a DefenseClaw-governed OpenClaw instance.
In production, these values come from the Gram dashboard.

Used by:
  - verify_config.py (local sanity check)
  - start.sh (reads the same values from env vars inside Docker)
"""
import os
from pathlib import Path

# Load .env.local from repo root if it exists
_env_local = Path(__file__).resolve().parent.parent / ".env.local"
if _env_local.exists():
    for line in _env_local.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            key, _, value = line.partition("=")
            os.environ[key.strip()] = value.strip()

# ---------------------------------------------------------------------------
# Gram API
# ---------------------------------------------------------------------------
GRAM_SERVER_URL = os.environ.get("GRAM_SERVER_URL", "https://app.getgram.ai")
GRAM_PROJECT_SLUG = os.environ.get("GRAM_PROJECT_SLUG", "ryan")
GRAM_API_KEY = os.environ.get("GRAM_API_KEY", "")

# ---------------------------------------------------------------------------
# DefenseClaw guardrail proxy
# ---------------------------------------------------------------------------
GUARDRAIL = {
    # Where the guardrail proxy forwards LLM completions (Gram's endpoint)
    # OpenAI-compatible, models need provider/ prefix (e.g. anthropic/claude-sonnet-4.5)
    "upstream_url": f"{GRAM_SERVER_URL}/chat/completions",
    # Local proxy listen address (host-side of veth, inside container)
    "listen_host": "10.200.0.1",
    "listen_port": 4000,
    # Policy template
    "policy": "default",  # default | strict | permissive
}

# ---------------------------------------------------------------------------
# OpenShell sandbox — network policy
#
# These endpoints are reachable from inside the OpenShell sandbox.
# Traffic goes through OpenShell's HTTP CONNECT proxy on 10.200.0.1:3128,
# which evaluates the OPA Rego policy per-connection.
#
# Connections to hosts NOT listed here are denied by the policy engine.
# No iptables needed — enforcement is at the proxy layer via OPA/Rego.
# ---------------------------------------------------------------------------
OPENSHELL_NETWORK_POLICY = {
    # DefenseClaw sidecar (host-side of veth pair)
    "allow_defenseclaw_sidecar": {
        "binaries": [{"path": "/**"}],
        "endpoints": [
            {"host": "10.200.0.1", "ports": [18970, 4000], "tls": "skip"},
        ],
    },
    # Gram server — MCP servers + completions + telemetry all go here
    "allow_gram": {
        "binaries": [{"path": "/**"}],
        "endpoints": [
            {"host": "app.getgram.ai", "ports": [443], "tls": "skip"},
        ],
    },
    # npm registry — needed for OpenClaw skill/package installs
    "allow_npm_registry": {
        "binaries": [{"path": "/**"}],
        "endpoints": [
            {"host": "registry.npmjs.org", "ports": [443], "tls": "skip"},
        ],
    },
    # PyPI — needed for pip installs inside sandbox
    "allow_pypi": {
        "binaries": [{"path": "/**"}],
        "endpoints": [
            {"host": "pypi.org", "ports": [443], "tls": "skip"},
            {"host": "files.pythonhosted.org", "ports": [443], "tls": "skip"},
        ],
    },
    # example.com — allowed for POC testing
    "allow_example": {
        "binaries": [{"path": "/**"}],
        "endpoints": [
            {"host": "example.com", "ports": [80, 443], "tls": "skip"},
        ],
    },
}
# Endpoints that are explicitly NOT allowed (agent can't reach these):
# - LLM providers (api.anthropic.com, api.openai.com) — forced through guardrail proxy
# - Arbitrary internet — denied by OPA policy (no matching network_policies entry)
# - asdf.com — intentionally NOT in the policy (used to test blocking)
