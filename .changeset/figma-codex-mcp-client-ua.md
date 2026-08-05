---
"server": patch
---

Stop rejecting current Codex clients at the Figma MCP allowlist (DNO-765).
Codex renamed its MCP client User-Agent — 0.144 sent `codex_cli_rs/…`, the
0.146 unified-app build sends `codex-mcp-client/…` — and the allowlist only
carried the old token, so every Codex → Figma MCP call proxied through Gram
was rejected as an unapproved client. Both tokens are now listed so neither
older deployed clients nor current ones are blocked.
