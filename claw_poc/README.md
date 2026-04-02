# DefenseClaw x Gram POC (Fly.io)

Provisions an OpenClaw agent inside an **OpenShell sandbox** on a **Fly.io Firecracker VM**, pre-configured with a Gram tenant's MCP servers. LLM calls route through Gram's completions endpoint. Network egress is enforced by OpenShell's OPA/Rego policy engine via HTTP CONNECT proxy.

## Stack

- **OpenClaw** (npm) — AI agent runtime with gateway + MCP support
- **DefenseClaw** (GitHub: cisco-ai-defense/defenseclaw) — governance layer (Go binary + Python CLI)
- **OpenShell** (NVIDIA) — sandbox providing namespace isolation, Landlock LSM, seccomp-BPF, and OPA-enforced network policy
- **Gram** (app.getgram.ai) — hosts MCP servers + OpenAI-compatible `/chat/completions` endpoint
- **Fly.io** — Firecracker microVM runtime (not a Docker container — OpenShell runs natively)

## Architecture

```
Fly.io Firecracker VM
├── Host side (10.200.0.1) — root
│   ├── start.py (entrypoint — provisions everything)
│   ├── iptables (DNS forwarding, gateway port DNAT)
│   └── openshell-sandbox process
│       └── HTTP CONNECT proxy on :3128 (OPA policy enforced)
└── OpenShell sandbox (10.200.0.2) — sandbox user
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
| `config.py` | Gram credentials for local scripts. Loads API key from `../.env.local`. |
| `verify_config.py` | Sanity checks credentials: verifies API key, MCP servers, completions, tool calls. |
| `poc.py` | Full end-to-end: destroys/creates Fly app, deploys, sends prompts, tests network policy. |
| `start.py` | VM entrypoint: fetches MCP servers from Gram API, generates OpenShell policy YAML, starts sandbox. |
| `start-openclaw.sh` | Runs INSIDE the sandbox: sets proxy env vars, starts gateway, runs network policy tests. |
| `grant-scopes.py` | Baked into image. Waits for paired.json, grants full operator scopes to auto-paired devices. |
| `test-network-policy.py` | Baked into image. Runs inside sandbox to test allow/deny via OPA proxy (process ancestry matters). |
| `Dockerfile` | Multi-stage build: DefenseClaw Go binary, openshell-sandbox from NVIDIA OCI, OpenClaw + runtime. |
| `fly.toml` | Fly.io app configuration. |

## Setup

```bash
# 1. Authenticate with Fly.io
fly auth login

# 2. Create .env.local in the gram repo root with your API key
echo "GRAM_API_KEY=gram_live_xxx" > ../.env.local

# 3. Verify credentials work
uv run python verify_config.py

# 4. Run the full POC (deploys to Fly, sends prompts, tests network policy)
uv run python poc.py
```

## What it tests

1. **MCP tool call** — agent calls `pizza-map` tool via Gram's MCP gateway
2. **Chat completions** — LLM calls route through `Gram-Key` auth to Gram's `/chat/completions`
3. **Network allow** — `example.com` is in the OpenShell policy, curl succeeds (via OPA-enforced proxy)
4. **Network block** — `asdf.com` is NOT in the OpenShell policy, proxy denies the CONNECT request

## Why Fly.io instead of Docker?

Fly runs Firecracker microVMs, not containers. This means:
- OpenShell runs **natively** — no `--privileged`, no nested namespaces
- No Docker-specific iptables bridging between container and sandbox namespaces
- Each tenant gets a fully isolated VM with its own kernel namespace
- Fly handles VM lifecycle, auto-stop/start, and persistent volumes
