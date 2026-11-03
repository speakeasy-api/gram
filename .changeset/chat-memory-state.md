---
"server": minor
---

Agent session listings now report each session's Organizational Memory state (remembered, not summarized, excluded or personal). New `chat.getMemory` and `chat.setMemoryExcluded` endpoints show the masked summary memory reads, the memos saved from a session and the day digest that folded it, and let people keep a session out of memory. Anyone who can read a session can exclude it; returning someone else's session needs `chat:write`.
