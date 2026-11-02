---
"server": patch
---

Code Mode now reports a program that exceeds its 8 MiB tool reply budget with the `callback_limit` error, whichever side of the runtime detects it. The runner counts only tool replies, as the gateway does, so large tool arguments no longer end a run whose replies fit the budget.

A program that reaches the 30-second execution deadline now always ends with `deadline_exceeded`. It could previously end with `outcome_unknown` ("runtime disconnected") instead. In both cases `tool_calls` keeps each call's recorded outcome.
