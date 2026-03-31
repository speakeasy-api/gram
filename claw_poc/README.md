# DefenseClaw x Gram POC

Provisions an OpenClaw agent inside Docker, pre-configured with a Gram tenant's MCP servers. LLM calls route through Gram's completions endpoint. Network egress is locked down via iptables to only allowed hosts.

## Stack

- **OpenClaw** (npm) — AI agent runtime with gateway + MCP support
- **DefenseClaw** (GitHub: cisco-ai-defense/defenseclaw) — governance layer (Go binary + Python CLI)
- **Gram** (app.getgram.ai) — hosts MCP servers + OpenAI-compatible `/chat/completions` endpoint
- **mcp-remote** (npm) — stdio-to-HTTP bridge (OpenClaw's npm build only supports stdio MCP transports)
- **iptables** — container-level egress firewall enforcing the network allow-list

## Files

| File | Purpose |
|------|---------|
| `config.py` | All provisioning inputs: Gram credentials, project, guardrail settings, network policy. Loads API key from `../.env.local`. |
| `verify_config.py` | Sanity checks credentials: verifies API key, lists MCP servers, tests a completion, tests a tool call. Run locally before Docker. |
| `poc.py` | Full end-to-end: tears down old container, builds image, starts it, sends prompts, tests network policy enforcement. |
| `start.sh` | Container entrypoint: fetches MCP servers from Gram API, configures OpenClaw + Gram as LLM provider, applies iptables rules, starts gateway. |
| `Dockerfile` | Builds from GitHub sources (DefenseClaw + OpenClaw + mcp-remote). |

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
3. **Network allow** — `example.com` is in the policy, fetch succeeds
4. **Network block** — `asdf.com` is NOT in the policy, fetch is blocked by iptables
