---
"server": minor
---

Operators can register an MCP client against a user session issuer for a client that cannot register itself. Speakeasy mints the client ID and secret, returns the secret once, and stores only its hash. Operators can also replace the redirect URIs of a registered client, except a CIMD client, whose metadata document sets them. Both need project:write on the issuer's project, or org:admin for an organization-owned issuer, and both are recorded in the audit log.
