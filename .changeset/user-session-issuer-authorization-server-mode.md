---
"server": minor
"dashboard": patch
---

Show where a user session issuer's MCP servers send clients to authorize. Issuers now report their authorization server mode (one per MCP server, or one shared by the issuer), whether the shared issuer URL is pinned, and, for a shared authorization server, its issuer URL and token endpoint or why it is not served. Organization administrators can list project-specific issuers alongside organization-owned ones with a `tier` filter, read any issuer in the organization, and list the MCP servers attached to an issuer. Migration preflights now warn when consolidating changes the authorization server that clients and identity providers are configured with.
