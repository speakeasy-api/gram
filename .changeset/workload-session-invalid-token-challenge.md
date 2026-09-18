---
"server": patch
---

An MCP request carrying a workload session token that has been revoked or has expired, or whose workload no longer has an active assigned agent, now gets a `401` with `WWW-Authenticate: Bearer ..., error="invalid_token"`, so the client drops the token and requests a new one instead of retrying with it. A tool denied by the assigned agent's policy still returns the usual permission error, and human sessions keep their existing challenge.
