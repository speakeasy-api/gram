---
"server": minor
"dashboard": patch
---

`chat.load` now accepts a producer-scoped API key (`Gram-Key`) in addition to a dashboard session and a chat-session token, so backend integrations can pull chat transcripts programmatically without a browser session. An API key is treated as a first-party project credential: like the dashboard session (and the way RBAC already exempts API keys via `ShouldEnforce`), it can load any chat in its project, including chats owned by an external user. External-user and chat-session-token callers remain owner-matched, and the project boundary still applies. The dashboard's producer key-scope description now notes it can export chat transcripts.
