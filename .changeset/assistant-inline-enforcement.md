---
"server": minor
---

Hosted assistant tool calls now consult the same realtime risk-policy and spend-gate enforcement used for hook-ingested agent traffic, at the runner's tool-dispatch chokepoint. Public hook ingest still rejects the reserved `assistant` adapter so clients cannot forge that attribution.
