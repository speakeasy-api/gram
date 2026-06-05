---
"@gram-ai/elements": patch
---

Strip `<message-context>` source-adapter framing when converting stored chat messages for display, and skip turns that are pure framing (e.g. an MCP auth event with no human text). Previously, reopening a historical assistant thread rendered the first message as a raw `<message-context>` block that exposed internal event/auth metadata.
