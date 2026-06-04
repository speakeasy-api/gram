---
"server": minor
---

Add `POST /rpc/assistants.kickoffMessage`: nudges the project assistant to proactively greet a returning dashboard user. It enqueues a _hidden_ turn — the server-owned greeting prompt reaches the model (so it emits a short welcome-back recap of the existing thread) but is never written to the user-visible conversation log; only the assistant's reply surfaces. Gated by project read access; pass the conversation's current correlation id so the greeting lands inside that thread. Foundation for the AGE-2631 sidebar welcome-back.
