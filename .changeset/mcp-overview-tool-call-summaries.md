---
"server": minor
"dashboard": patch
---

`telemetry.getObservabilityOverview` takes `tool_calls_only`. When set, chat, token, cost and session duration figures come back as 0, and overviews of two hours or more can be read from pre-summed tool call counts instead of raw logs, with periods aligned to 15-minute boundaries.
