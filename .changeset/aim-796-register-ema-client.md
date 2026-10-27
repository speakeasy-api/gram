---
"server": minor
"dashboard": patch
---

Organization administrators can register an MCP client for enterprise-managed authorization on an organization-level sign-in issuer, through `organizationUserSessionIssuers.createJwtBearerClient` or the `register_ema_client` Platform MCP tool. The client redeems identity assertions with the jwt-bearer grant and has no redirect URIs. Confidential clients get a generated secret, returned once and stored hashed. Client creation is audited.
