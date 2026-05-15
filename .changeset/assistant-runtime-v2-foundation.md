---
"server": minor
---

Lay the groundwork for the v2 assistant runtime path: optional `ThreadID` claim on assistant runtime tokens (assistant-scoped tokens omit it), a `runtime_version` column plus partial unique index on `assistant_runtimes`, a new `/rpc/assistants.getThreadBootstrap` endpoint that lets a runner pull a thread's bootstrap state on demand, and an assistant-scope check on `/chat/completions` that rejects writes whose `Gram-Chat-ID` resolves to a chat outside the caller's assistant. Existing v1 admit, configure, and run-turn flows are unchanged.
