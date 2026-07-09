---
"dashboard": patch
"server": patch
---

Restrict the Observe dashboard section (Costs, MCP & Tools Insights, Employee Enrollment, Agent Sessions, Tool Logs) to org admins. The Observe nav stays visible (like the Secure section), but each Observe page is gated on `org:admin`, so basic members see an "Access restricted" notice. Basic members also no longer receive `environment:read` by default.
