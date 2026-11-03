---
"server": patch
---

The Platform MCP has a new `remember` tool for organizations with Organizational Memory turned on in the staff admin dashboard. It saves a decision, convention or gotcha to a project's Organizational Memory. Detected secrets are masked before the memo is stored, the memo is attributed to whoever saved it, and every save is audited. It needs `memory:write` on the project: organization admins have it by default, members need a grant.
