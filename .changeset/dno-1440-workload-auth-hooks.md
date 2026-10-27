---
"server": patch
---

The Speakeasy hooks binary gains a `workload_auth` setting (also `SPEAKEASY_AI_HOOKS_WORKLOAD_AUTH=1`) for sandboxes whose egress proxy authenticates requests as the agent's workload, such as Claude Tag's Agent Proxy. With no API key available, it sends events without `Gram-Key` and the proxy attaches the workload's device-agent token, so the events are recorded as the agent with no Speakeasy API key configured. Plugins the server generates keep authenticating with their baked key.
