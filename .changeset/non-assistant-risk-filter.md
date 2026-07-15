---
"dashboard": patch
"server": patch
---

feat(risk): add an assistant filter to risk events. The Risk Events page gains an "Assistant" select listing the project's assistants plus a "No assistant" option, so findings from chats not linked to an assistant (the ones most likely missing user attribution) can be surfaced on their own — or scoped to a single assistant. API: `assistant_id` and `non_assistant` params on `listRiskResults`/`listRiskResultsForAgent`.
