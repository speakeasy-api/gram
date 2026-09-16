---
"server": patch
---

Add a `workload` principal type and give workload sessions a real actor. A workload holds no grants of its own, so a grant written against a `workload:` principal is refused; authority reaches it through its assigned agent. A `workload:` session subject carries its workload principal on the request context and admits through that agent, so authorization evaluates the machine instead of refusing it. The workload never resolves through user principals, so it cannot pick up the organization-wide `user:all` grants. Workload sessions follow the agent authorization rollout, stop authorizing once their issuer is deleted, and their authorization challenges are listed with a `workload` principal type.
