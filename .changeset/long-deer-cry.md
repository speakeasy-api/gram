---
"@gram-ai/opencode-observability": minor
---

Add the Gram observability plugin for opencode. It maps opencode plugin events (sessions, tool calls, prompts, assistant turns, permission asks) to Gram's canonical hook vocabulary and ships them to `POST /rpc/hooks.ingest` — the same provider-neutral endpoint used by Claude Code, Cursor, and Codex. Delivery is fail-open (bounded timeout + retry, errors swallowed so a dead network never blocks the session) and idempotent. It resolves MCP tool identity (rewriting opencode's `<server>_<tool>` names into the `mcp__<server>__<tool>` form plus server url/command) so MCP calls are attributed and shadow-MCP scanned rather than misclassified as native tools, and forwards model id, token, and cost usage plus device hostname for the dashboard's usage and data-flow views. Configured via env vars (`GRAM_KEY`, `GRAM_PROJECT`, optional `GRAM_URL`/`GRAM_USER_EMAIL`).
