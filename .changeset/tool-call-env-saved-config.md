---
"server": patch
---

HTTP tool calls now keep saved configuration (saved environments and resolved OAuth access tokens) apart from `MCP-*` request headers. A server URL sent as a request header is used only when the server has no system or saved configuration, and an OAuth token URL sent as a request header only when the client ID and secret are request headers too; otherwise the configured URL applies. Tool call arguments no longer set environment variables.
