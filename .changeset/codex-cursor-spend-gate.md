---
"server": minor
---

Extend spend-gate enforcement to Codex and Cursor at parity with Claude. Over-budget actors are now denied on the legacy provider endpoints (`hooks.codex`: PreToolUse, PermissionRequest, UserPromptSubmit; `hooks.cursor`: preToolUse, beforeMCPExecution, beforeSubmitPrompt) and on the unified `hooks.ingest` path for the codex and cursor adapters — previously the ingest spend gate was Claude-only even though risk scanning already ran adapter-agnostically there. Tool-call spend denies carry a durable block page link on every provider; the gate keeps running before any risk-policy evaluation and keeps failing open on infrastructure errors. opencode still passes through pending a product decision on its enforcement surface.
