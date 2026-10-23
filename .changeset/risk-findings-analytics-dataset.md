---
"server": minor
"dashboard": minor
---

Explore has a `risk_findings` dataset, so widgets and dashboards can count what the risk policies caught: by category, rule, detector, severity, policy, agent, team and person. It answers counts only, never individual findings, and leaves out the matched values and the chats they came from. Like the rest of the risk surfaces it is for organization administrators: describe lists it to them alone, queries, value lists and widget saves refuse anyone else, and a shared widget over it tells other viewers it is for org admins instead of reading as broken. A Speakeasy-built "Security & risk" dashboard shows findings, high and critical findings, people affected, findings over time by category, top rules, and findings by agent, person and policy. The Platform MCP analytics tools follow the same rules.
