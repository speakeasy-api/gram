---
"server": patch
---

Tool calls now keep saved configuration (saved environments and resolved OAuth access tokens) apart from `MCP-*` request headers.

- A server URL sent as a request header is used only when the server has no system or saved configuration.
- An OAuth token URL override is used only when it comes from the same layer as the client ID and secret, or a lower one: a request header needs both sent as headers, and a saved environment's needs neither set in system environment. Otherwise the configured URL applies.
- A cached client credentials token is reused only by calls with the same client secret.
- A variable set to system-provided or to none is no longer taken from a request header, on servers with or without a default environment.
- For function tools and resources, a request header no longer replaces a value that system environment or saved configuration sets, unless the variable is set to user-provided.
- Resource reads apply per-variable provider settings to configured values the same way tool calls do.
- Tool call arguments no longer set environment variables.
