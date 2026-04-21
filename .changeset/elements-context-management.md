---
"@gram-ai/elements": minor
---

Add two context-length management primitives to prevent upstream "prompt is
too long" errors on long tool-heavy conversations:

- `ElementsConfig.tools.maxOutputBytes` — cap the UTF-8 byte size of any
  single MCP tool call's result. Oversized results are truncated with a
  head+tail preserving strategy and a notice suffix before entering
  conversation history. Disabled by default; opt in per-page.
- `ElementsConfig.contextCompaction` — auto-compact older turns when the
  estimated token count passes a fraction of the model's context ceiling
  (default 70%). System prompt and the most recent turns are preserved; a
  synthetic marker is inserted so the model knows older context was elided.
  Enabled by default; `disabled: true` to opt out.
