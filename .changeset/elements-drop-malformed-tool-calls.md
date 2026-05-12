---
"@gram-ai/elements": patch
---

Drop persisted tool calls that arrive without a `toolCallId` instead of giving them an empty-string id. Previously two such parts in the same restored thread would alias under the same key and the runtime would throw "Tool call argsText can only be appended, not updated" while loading the chat.
