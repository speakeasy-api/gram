---
"server": minor
---

Add evidence change-detection columns to MCP approval requests: `evidence_changed_at` flags a permission-relevant drift from the evidence the latest approval rested on, and `notified_change_fingerprint` makes the daily recheck announce each distinct change once.
