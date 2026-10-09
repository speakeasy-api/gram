---
"server": minor
---

A shared authorization server now accepts a workload grant that names no resource, as Claude Tag sends when its Resource field is left empty. The token it issues works at every MCP server of the issuer that the workload's assigned agent may connect to, and a server the agent may not connect to answers 403 `insufficient_scope` while the token keeps working elsewhere. A grant naming a resource still returns a token for that one server, and the Claude Tag guided setup now asks to leave Resource empty.
