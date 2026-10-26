---
"server": patch
---

Code Mode now reports a program that exceeds its 8 MiB tool reply budget with the `callback_limit` error, whichever side of the runtime detects it. The runner counts only tool replies, as the gateway does, so large tool arguments no longer end a run whose replies fit the budget.
