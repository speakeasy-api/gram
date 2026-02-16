---
"server": patch
---

Attempt OAuth discovery for MCP servers returning AuthRejectedError. Previously when a user adds a catalog MCP server without OAuth2.1 (like HubSpot) to their project and opens it 
in the playground, there's no way to configure authentication — the AUTHENTICATION section is completely missing. This happens because the server returns `401` without a `WWW-Authenticate header` (or `403`) 
during the initial connection probe, which triggers the `AuthRejectedError` path. That path currently just logs and continues, storing zero auth metadata. The frontend then sees no OAuth config and no header
definitions, so it shows "No authentication required." Servers like linear with Oauth2.1 works correctly because its MCP server returns 401 with a WWW-Authenticate header, triggering the `OAuthRequiredError` path which runs full OAuth discovery.
