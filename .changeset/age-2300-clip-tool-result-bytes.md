---
"server": patch
---

The assistant runtime now spills oversized MCP tool results to a file inside the assistant workdir instead of letting them 413 the provider. The in-band tool result is replaced with a pointer (`{ truncated, saved_to, original_bytes }`) so the model can read or grep the full output via the filesystem tools — no information loss, no provider error.
