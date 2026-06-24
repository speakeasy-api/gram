---
"@gram-ai/elements": minor
"dashboard": minor
"server": minor
---

feat(dashboard): search within a chat thread. The chat detail sheet gains a find-in-conversation bar backed by full-thread server-side text search (`chat.load` `query` param returns the messages matching the query plus surrounding context, mirroring the risk-windowed view). Jump between matches with the prev/next controls or Enter/Shift+Enter (wrapping at the ends), Escape clears. The active match is highlighted bright yellow and the rest pale — across message text, tool names, and tool argument/output sections — and the tool holding the active match expands, collapsing again as you navigate away.
