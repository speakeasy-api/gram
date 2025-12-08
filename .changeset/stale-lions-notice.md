---
"dashboard": patch
"server": patch
---

- fix SSE streaming response truncation due to chunk boundary misalignment
- `addToolResult()` was called following tool execution, the AI SDK v5 wasn't automatically triggering a follow-up LLM request with the tool results. This is a known limitation with custom transports (vercel/ai#9178).
