---
"server": patch
---

Connecting to a remote MCP server now sends its RFC 8707 resource indicator exactly as the server is registered, trailing slash included. Providers that match the resource exactly against their published RFC 9728 resource, such as one publishing `https://host/`, no longer reject sign-in because Gram sent `https://host`.
