---
"@gram-ai/elements": patch
---

Keep frontend tools executable inside streamText's multi-step loop. Build the AI SDK `ToolSet` directly from `FrontendTool` definitions so `execute` survives, and stop double-approval-wrapping them (`defineFrontendTool` already self-wraps).
