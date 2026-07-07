---
"@gram-ai/elements": patch
"dashboard": patch
---

Fixed chat MCP tool connections failing with an "Illegal invocation" fetch error, which left chats without their configured MCP tools (including the assistant setup chat, which could no longer call the assistant's own tools). Also fixed opening a chat via a shared `?threadId=` URL sometimes silently landing on a new empty thread instead of restoring the linked conversation.
