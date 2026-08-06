---
"server": minor
"dashboard": patch
---

Extend spend-gate enforcement to Codex and Cursor at parity with Claude. Over-budget actors are now denied on the legacy provider endpoints (`hooks.codex`: PreToolUse, PermissionRequest, UserPromptSubmit; `hooks.cursor`: preToolUse, beforeMCPExecution, beforeSubmitPrompt) and on the unified `hooks.ingest` path for the codex and cursor adapters (case-insensitive match) — previously the ingest spend gate was Claude-only even though risk scanning already ran adapter-agnostically there. Cursor MCP calls are spend-gated exactly once (at beforeMCPExecution, mirroring the risk-scan dedup), tool-call spend denies mint a durable block page whose link rides the deny reason, idempotent redeliveries keep the deny without minting duplicate block rows, and the block page headline falls back to spend-rule framing instead of rendering an empty policy name. The gate keeps running before any risk-policy evaluation and failing open on infrastructure errors; opencode still passes through pending a product decision on its enforcement surface.
