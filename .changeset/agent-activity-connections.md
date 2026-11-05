---
"server": patch
"dashboard": patch
---

An agent's Activity tab now shows when someone grants it their connection or removes it, and says what each permission change touched. Attaching or detaching a connection also records `agent:connection_attach` or `agent:connection_detach` with the agent as subject, next to the existing `remote_session:attach` and `remote_session:detach` entries. A permission change row now reads, for example, "mcp:connect · Acme Ops" instead of only "Added direct policy grant to agent".
