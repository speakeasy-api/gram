---
"server": minor
"dashboard": minor
---

Add Codex (OpenAI) hooks support. A new `/rpc/hooks.codex` endpoint accepts all six Codex hook events (SessionStart, PreToolUse, PermissionRequest, PostToolUse, UserPromptSubmit, Stop), enforces org-level risk policies on blocking events, and records telemetry to ClickHouse. The plugin generator now produces a downloadable Codex observability plugin (ZIP and install script) that registers the hooks with a Gram marketplace entry in `~/.codex/config.toml`. The install instructions dialog gains a Codex tab alongside Claude Code and Cursor.
