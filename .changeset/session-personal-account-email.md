---
"server": patch
"dashboard": patch
---

feat: surface the AI account email on agent sessions. `chat.listChats` and `chat.load` now return `account_email` from the linked AI account, and the dashboard shows the personal account's email (e.g. a gmail on Claude Max) on session list rows, the transcript's user messages, and the session details popover — instead of only the attributed employee's work email.
