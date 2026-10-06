---
"server": patch
---

The Platform MCP no longer says sign-in needs manual setup for an MCP server whose OAuth provider supports client ID metadata documents but not dynamic client registration. `inspect_mcp_candidate` now classifies such a provider as automatic sign-in setup (`oauth_discovery: available_cimd`) and reports which automatic path applies in a new `automatic_client_registration` field. `attach_platform_mcp_identity_provider` connects these providers through a client ID metadata document, the same path the dashboard's automatic setup uses, instead of refusing them. Providers that offer dynamic client registration keep using it.
