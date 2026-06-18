---
"server": patch
---

Improve failure messaging for plugin and server-generated hooks. The Cursor hook now fails closed (emits a `deny` with a readable reason) when Gram is unreachable instead of silently allowing the call, surfaces missing credentials, and accepts both `GRAM_HOOKS_*` and legacy `GRAM_API_KEY`/`GRAM_PROJECT_SLUG` env vars. The Claude hook now explains `mktemp` failures instead of blocking with an empty reason, and the fire-and-forget MCP inventory and identity scripts gain an opt-in `GRAM_HOOKS_DEBUG=1` channel that reports why inventory or user attribution was skipped.
