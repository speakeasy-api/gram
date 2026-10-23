---
"dashboard": minor
"server": patch
---

Add tool approvals to Guardrails: a "Require approval for tool calls" use case with its own Tools, Who and Review steps (approvers are specific people or roles), a Require approval badge with the approver count in the policy list, and a Tool approvals tab where approvers see held calls with each argument on its own row and the agent's reason, and approve once or deny. The demo org seeds an approval policy with pending, used, denied and expired requests. Behind the `risk-tool-approvals` flag.
