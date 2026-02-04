---
"@gram-ai/elements": patch
---

Fix tool group count showing inflated numbers when loading chat history. The server accumulates all tool calls from a turn into each assistant message, causing duplicate tool-call parts when converting messages for the UI. Added deduplication in the message converter so each tool call only appears once. Also fixed `buildAssistantContentParts` silently dropping tool calls when assistant content is a string.
