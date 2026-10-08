---
"server": minor
---

Remote MCP servers behind a remote session client that holds its own upstream credential (`credential_owner = 'self'`) now reach the upstream with a token obtained through the OAuth client credentials grant, for every caller. Callers never see a connect step for such a client. When the upstream rejects the token, Speakeasy obtains a new one and retries once; if that also fails, or the token cannot be obtained, the caller gets an error naming the MCP server administrator (or asking for a retry during an outage) instead of a reauthorization challenge. A rejected client is flagged with `upstream_rejected_at`.
