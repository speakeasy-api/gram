---
"server": minor
---

Ingest opencode observability events natively. The hook ingest pipeline recognizes the `opencode` source (`parseOpencodeHookEvent`), giving opencode events native event-name fidelity instead of a generic fallback, and records opencode model/token/cost usage rows for the telemetry model-usage, token, and cost widgets.
