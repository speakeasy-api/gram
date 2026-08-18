---
"server": minor
"dashboard": patch
---

Session moves now record a lineage edge linking the original session to its continuation. The device agent can pass the continuation's session id in `agent.reportSessionMoved`, a new `chat.listSessionLinks` endpoint resolves the edges touching a set of chats, and the Agent Sessions detail panel shows a "Linked sessions" section — "Moved to Cursor" on the original, "Derived from …" on the continuation, with navigation between the two when both are captured.
