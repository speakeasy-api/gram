---
"server": patch
---

Platform MCP can now pause and resume one data export route. Only the route's on/off state changes; its data source and destination are left untouched, resuming a route whose destination is missing or deleted is refused, and each change is recorded in the audit log as `data_export_route:pause` or `data_export_route:resume`. Data produced while a route is paused is dropped, not delivered later.
