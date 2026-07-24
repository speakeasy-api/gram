---
"server": minor
"dashboard": minor
---

Requests and approvals now understand allow-all shadow MCP policies. Approving a bypass request (from the approvals page or the inventory review flow) on an allow_all policy revokes the server URL's risk_policy:block grant — a project-wide unblock with no principal-scoped bypass grants. Revoking restores the block grant; denying leaves it untouched. The request status change is audited like every other bypass-request decision. The approval UIs skip the audience/policy pickers for these requests and explain that approval unblocks the server for everyone in the project.
