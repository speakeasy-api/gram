---
"server": patch
---

Improve failure handling and diagnostics for plugin and server-generated hooks.

- The Cursor hook now fails closed (emits a `deny` with a readable reason) when Gram is unreachable or returns an error, instead of silently allowing the call and bypassing blocking policies. Only a `2xx` is treated as a decision; a `3xx` (e.g. an unfollowed redirect) now fails closed too.
- Hook success is restricted to `2xx` across the Claude and Cursor hooks (previously `2xx`–`3xx`).
- The Cursor hook surfaces missing credentials, accepts both `GRAM_HOOKS_*` and legacy `GRAM_API_KEY`/`GRAM_PROJECT_SLUG` env vars, and passes its API key via a mode-`600` curl config file instead of the command line.
- The Claude hook now explains `mktemp` failures instead of blocking with an empty reason.
- The MCP inventory payload is sent on stdin (`--data-binary @-`) instead of as a command-line argument, so large inventories no longer risk an `ARG_MAX` failure that silently drops telemetry.
- The fire-and-forget MCP inventory and identity scripts gain an opt-in `GRAM_HOOKS_DEBUG=1` channel that reports why inventory or user attribution was skipped.
