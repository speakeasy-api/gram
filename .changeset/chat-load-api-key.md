---
"server": minor
"dashboard": patch
---

`chat.load` now accepts a producer-scoped API key (`Gram-Key`) in addition to a dashboard session and a chat-session token, so backend integrations can pull chat transcripts programmatically without a browser session. Only a **direct** producer API key is treated as a first-party project credential: like the dashboard session (and the way RBAC already exempts API keys via `ShouldEnforce`), it can load any chat in its project, including chats owned by an external user. External-user callers and chat-session tokens stay owner-matched even when the token carries the minting key's `APIKeyID`, and the project/org boundary still applies. The dashboard's producer key-scope description now notes it can export chat transcripts, and the endpoint is added to the public SDK/docs allowlist so its API-key auth is captured in the published API docs.
