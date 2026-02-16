---
"@gram-ai/elements": minor
---

Add `dangerousApiKey` auth option for quick dev/testing without a backend session endpoint. The client exchanges the API key for a session token automatically, with a red warning banner reminding users not to ship this to production.

Also introduce a unified `session` field on `ApiConfig` that accepts either a static token string or an async fetcher function, consolidating the previous `sessionToken` and `sessionFn` fields (both still work but are now deprecated).
