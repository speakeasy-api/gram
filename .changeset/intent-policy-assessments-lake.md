---
"server": minor
---

Each intent-policy analysis now records its results. Every tool call judged on the intent policies analysis page is published as a `gram.intentpolicy.v1.Assessment` event, and `gram streams` writes those events to the lake as daily-partitioned Parquet under `gram.intentpolicy.v1.LakePrimary/`. Each row says who ran the analysis (organization, project, user), which session and tool call it judged, the guardrail version, the evaluator and model, every label and contract outcome, the final decision, cost and latency, and when the call happened and when it was judged. Tool arguments are left out; a row carries only their size. Publishing never waits on Pub/Sub or fails the analysis.
