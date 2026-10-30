---
"server": patch
"dashboard": patch
---

An agent's MCP connections now show every way it signs in, not only MCP sign-ins that chose the agent. A used API key shows with when it was last used, and a workload that signs in as the agent through federation shows with its subject. `agents.listSessions` now also returns sessions of workloads assigned to the agent, marked with `subject_kind` and `workload_subject`. Key and workload rows link to the agent's credentials, where they are managed.
