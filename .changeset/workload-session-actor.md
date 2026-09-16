---
"server": patch
---

Give a workload session a real actor. A `workload:` subject now carries its workload principal on the request context and admits through the agent assigned to it, so authorization evaluates the machine instead of refusing it. The workload never resolves through user principals, so it cannot pick up the organization-wide `user:all` grants
