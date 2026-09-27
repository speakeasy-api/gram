---
"server": patch
---

Adds the `workload:read` and `workload:write` RBAC scopes, which gate managing an organization's workload identity trust policy: its issuers, its admitted subjects, and which agent each inherits its policy from. Both are admin system-role defaults and neither is a member default, since reading the policy discloses which machines an organization recognises. They are registered as agent-runtime scopes but deliberately not agent-runtime-safe, so a workload cannot use its inherited policy to admit further machines.
