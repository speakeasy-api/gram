---
"server": patch
---

Adds `allow_prefix_admission` to `workload_issuers`, and `match_kind` to `workload_identity_admissions` and `workload_agent_assignments`, so a workload subject can be admitted by a prefix of its `sub` where the issuer permits it. Every column defaults to today's behaviour, so no existing issuer, admission or assignment changes until an operator opts in.
