---
"server": minor
---

Administrators can create upstream credentials for an MCP server: a personal connection or service account over an existing OAuth registration, a static credential with stored headers, or a pass-through of a caller's header. An existing credential can be offered on more servers instead of copied, and an administrator-created credential's headers can be replaced to rotate it, keeping secret values that are not re-entered. Each change is audited, and the Platform MCP can offer an existing credential on another server. Credentials an administrator creates take effect where typed upstream authentication is enforced.
