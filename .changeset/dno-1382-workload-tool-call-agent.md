---
"server": minor
---

Tool calls made through workload identity now record the agent the workload acts for, in the new `gram.authorization.agent.id` attribute. The workload stays the actor. Agent-key calls record their own agent there too. Hook events drop a client-sent value for this attribute, as they already do for the actor attributes.
