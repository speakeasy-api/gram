---
"server": patch
---

Add a `workload` principal type and the lookup that resolves the agent a workload inherits its permission policy from. A workload holds no grants of its own, so a grant written against a `workload:` principal is refused; authority reaches it through its assigned agent
