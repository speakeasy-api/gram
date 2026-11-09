---
"server": patch
"dashboard": patch
---

A failed hook tool call now shows as failed in an agent session. Its result row is stored with an `error` finish reason, and a failure that carries no error text still gets a result row. The session drawer shows a red cross, a "Failed" label and the error as the call's output. A call with no result no longer gets a green check: it reads "Pending" for 10 minutes and "Unknown" after that, and a tool group with such a call is not marked complete.
