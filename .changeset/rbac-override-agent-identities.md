---
"server": patch
"dashboard": patch
---

Agent identity pages now work while a platform admin has the RBAC override turned on. Agent management no longer rejects requests that carry the override. Instead, the override narrows each agent permission check: the check must pass both your real grants and the override. Agent owners keep managing their own agents, and delegating access to an agent is still limited by your real grants.
