---
"server": patch
---

Add `mark_risk_findings_false_positive` and `unmark_risk_findings_false_positive` to the Platform MCP so an administrator can dismiss specific Watchdog findings, or restore them, from an agent. Both require confirmation, replay safely on an idempotency key, audit the acting user per finding, and return a receipt that partitions the requested ids into changed, already in the requested state, and not found.
