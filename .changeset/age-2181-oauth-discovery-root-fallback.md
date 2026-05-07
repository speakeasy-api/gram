---
"server": patch
---

Fix OAuth discovery for MCP servers that host well-known metadata at the origin root regardless of endpoint path (e.g. Atlassian). When the remote URL has a path and prior discovery strategies find no authorization server metadata, the discovery chain now retries both `/.well-known/oauth-protected-resource` and `/.well-known/oauth-authorization-server` probes against the origin root with the path stripped.
