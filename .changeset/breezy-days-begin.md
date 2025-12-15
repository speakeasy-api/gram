---
"dashboard": patch
"@gram/client": patch
"server": patch
---

Enable private MCP servers with Gram account authentication

This change allows private MCP servers to require users to authenticate 
with their Gram account. When enabled, only users with access to the 
server's organization can utilize it.

This is ideal for MCP servers that require sensitive credentials (such as API
keys), as it allows organizations to:

- Secure access to servers handling sensitive secrets (via Gram Environments)
- Eliminate the need for individual users to configure credentials during installation
- Centralize authentication and access control at the organization level
