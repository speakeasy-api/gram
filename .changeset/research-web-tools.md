---
"server": minor
---

The MCP research agent's web toolset lands as the `research` platform
toolset: `platform_web_search` runs cited web searches through OpenRouter's
web-search plugin on the org's chat key (tagged `mcp-research` for distinct
spend attribution), and `platform_fetch_page` fetches public pages through a
guardian-routed client with byte, redirect, and per-run fetch budgets, and
reduces HTML to readable text. Both tools are gated on the `mcp_approval`
feature and no assistant is granted the toolset by default — the research
agent runner attaches it explicitly. Their descriptions carry the standing
posture: everything returned is untrusted data to weigh and cite, never
instructions.
