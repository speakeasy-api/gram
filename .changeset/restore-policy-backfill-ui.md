---
"dashboard": patch
---

Restore the Backfill controls in the risk-policy progress sheet (`Backfill last N` with a numeric input + `Backfill all messages`). The controls were inadvertently dropped during a merge-conflict resolution after the original feature landed; the backend `TriggerRiskAnalysis.limit` field has been live throughout, so this only reconnects the UI.
