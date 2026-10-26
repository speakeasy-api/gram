---
"server": minor
"dashboard": patch
---

Add `risk.listApprovals`, `risk.getApproval` and `risk.decideApproval` for tool approval requests. Org admins see every request; other members see the requests they can approve and their own. Only a listed approver, signed in to the dashboard, may decide, and never the person who made the call. Requests and decisions are recorded in the audit log, and creating approval policies is gated behind the `risk-tool-approvals` flag.
