# DefenseClaw x Gram POC

Provisions an OpenClaw agent inside an **OpenShell sandbox**, pre-configured with a Gram tenant's MCP servers. LLM calls route through Gram's completions endpoint. Network egress is enforced by OpenShell's OPA/Rego policy engine via HTTP CONNECT proxy.

## Stack

- **OpenClaw** (npm) — AI agent runtime with gateway + MCP support
- **DefenseClaw** (GitHub: cisco-ai-defense/defenseclaw) — governance layer (Go binary + Python CLI)
- **OpenShell** (NVIDIA) — sandbox providing namespace isolation, Landlock LSM, seccomp-BPF, and OPA-enforced network policy
- **Gram** (app.getgram.ai) — hosts MCP servers + OpenAI-compatible `/chat/completions` endpoint
- **mcp-remote** (npm) — stdio-to-HTTP bridge (OpenClaw's npm build only supports stdio MCP transports)

## Architecture

```
Docker container
├── Host side (10.200.0.1)
│   ├── start.sh (entrypoint — provisions everything)
│   ├── iptables (DNS forwarding, gateway port forwarding)
│   └── openshell-sandbox process
│       └── HTTP CONNECT proxy on :3128 (OPA policy enforced)
└── OpenShell sandbox (10.200.0.2)
    ├── Network namespace (veth pair isolation)
    ├── Landlock LSM (filesystem access control)
    ├── seccomp-BPF (syscall filtering)
    └── OpenClaw gateway on :18789
        ├── MCP servers (from Gram, via mcp-remote)
        └── LLM completions (Gram, via proxy)
```

## Files

| File | Purpose |
|------|---------|
| `config.py` | All provisioning inputs: Gram credentials, project, guardrail settings, OpenShell network policy. Loads API key from `../.env.local`. |
| `verify_config.py` | Sanity checks credentials: verifies API key, lists MCP servers, tests a completion, tests a tool call. Run locally before Docker. |
| `poc.py` | Full end-to-end: tears down old container, builds image, starts it, sends prompts, tests network policy enforcement. |
| `start.sh` | Container entrypoint: fetches MCP servers from Gram API, generates OpenShell policy YAML, starts openshell-sandbox, applies post-sandbox iptables. |
| `start-openclaw.sh` | Runs INSIDE the OpenShell sandbox: sets HTTP proxy env vars, starts OpenClaw gateway. |
| `Dockerfile` | Multi-stage build: DefenseClaw Go binary, openshell-sandbox from NVIDIA OCI, OpenClaw + runtime. |

## Setup

```bash
# 1. Create .env.local in the gram repo root with your API key
echo "GRAM_API_KEY=gram_live_xxx" > ../.env.local

# 2. Verify credentials work
uv run python verify_config.py

# 3. Run the full POC (builds Docker, starts instance, sends prompts)
uv run python poc.py
```

## What it tests

1. **MCP tool call** — agent calls `pizza-map` tool via Gram's MCP gateway
2. **Chat completions** — LLM calls route through `Gram-Key` auth to Gram's `/chat/completions`
3. **Network allow** — `example.com` is in the OpenShell policy, fetch succeeds (via OPA-enforced proxy)
4. **Network block** — `asdf.com` is NOT in the OpenShell policy, proxy denies the CONNECT request
