---
"dashboard": patch
---

Two Project Assistant sidebar fixes:

- Make the server-assistant transport poll adaptively for replies: poll quickly for the first few iterations (so short turns surface within a few hundred milliseconds instead of waiting a full fixed interval), then back off geometrically to the steady-state interval for long, tool-heavy turns. Reduces the perceived latency of the project assistant relative to the old streaming AI assistant.
- Strip the backend's `<message-context>` framing (and drop framing-only turns) from the rendered transcript via Elements' new `history.transformChatMessage` hook, so reopening a historical thread no longer shows a raw block exposing internal event/MCP-auth metadata.
