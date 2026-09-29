---
"server": patch
---

Add `list_risk_findings`, `list_risk_findings_by_chat` and `get_risk_rule_breakdown` to the Platform MCP. The tools read the same store as the dashboard's Risk Events listing, redact every matched value to a length-and-hash fingerprint, pseudonymize user identities the way `list_watchdog_findings` does, page with opaque cursors bound to their filters, and share the row-level Watchdog read budget.
