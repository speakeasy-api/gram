---
"dashboard": patch
"server": patch
---

Add a dedicated `telemetry:read` scope that gates the Observe dashboard section (Costs, MCP & Tools Insights, Employee Enrollment, Agent Sessions, Tool Logs). Basic members no longer receive `telemetry:read` or `environment:read` by default. The Observe nav stays visible (like the Secure section); each Observe page is gated on `telemetry:read` OR `org:admin`, so existing admin roles keep access without a backfill while basic members see an "Access restricted" notice.
