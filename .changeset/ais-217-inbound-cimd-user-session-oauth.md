---
"server": minor
---

Support OAuth Client ID Metadata Documents (CIMD) on the Gram Session OAuth authorization server, gated per organization behind the `gram-user-session-cimd` feature flag. MCP clients that identify themselves with a URL-shaped `client_id`, such as Claude Code and VS Code, can now complete the OAuth flow without Dynamic Client Registration, including loopback redirects on any port.
