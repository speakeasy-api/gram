---
"server": minor
"admin": patch
---

Staff Admin MCP write tools now apply changes directly instead of through stored proposals and browser approval. Each tool is named after its operation, for example `set_organization_feature`. The server instructions and tool descriptions require agents to get explicit user confirmation before every change, and the staff event trail now records each write's operation and target.
