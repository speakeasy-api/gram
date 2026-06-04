---
"server": minor
---

Add `GET /rpc/assistants.listMessages`: reads a dashboard conversation log for a chat (the user's messages and the assistant's delivered replies), with `after_seq` for incremental polling. Enforces conversation ownership — since the conversation key is client-chosen and not project-unique, a caller may only read their own chat (a chat owned by another user reads as not-found). Completes the dashboard assistant round-trip. Foundation for AGE-2631.
