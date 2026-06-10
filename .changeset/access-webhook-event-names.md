---
"server": patch
---

Add generic access webhook event names for audit logs. Shadow MCP approval requests now emit `audit_log.access_request_event_v1`, and access rules emit `audit_log.access_rule_event_v1`; the previous Shadow MCP-specific event names remain in the webhook catalog with deprecated descriptions for compatibility.
