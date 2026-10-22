---
"server": patch
---

An MCP OAuth request whose `client_id` is the MCP server's own URL is now refused with `401 invalid_client` and a hint to check the MCP client's OAuth Client ID setting, instead of failing with a retryable `503` after trying to fetch that URL as a client metadata document.
