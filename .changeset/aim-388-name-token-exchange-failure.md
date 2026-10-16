---
"server": patch
---

When the code exchange at the end of an MCP connection login fails, the error now names why instead of only "upstream token exchange failed": the identity provider's standard OAuth error code (for example `invalid_grant` or `invalid_client`), including when the provider reports it under HTTP 200; the HTTP status of a failed response; or a fixed reason when the provider could not be reached, the MCP tunnel is not connected or could not reach the provider, or the client assertion could not be signed. The provider's own error description and vendor-specific codes are kept to server logs. A provider outage now answers 502 or 503 instead of 401. Federated sign-in now treats `temporarily_unavailable` and `server_error` from the provider as an outage, and `invalid_client` or `unauthorized_client` reported under HTTP 200 as a configuration problem.
