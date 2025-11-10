---
"server": patch
"cli": minor
---

feat: Add gram install command for MCP server configuration & support common clients

**Automatic Configuration**

```bash
gram install claude-code --toolset speakeasy-admin
```

- Fetches toolset metadata from Gram API
- Automatically derives MCP URL from organization, project & environment or custom MCP slug
- Intelligently determines authentication headers and environment variables from toolset security config
- Uses toolset name as the MCP server name

**Manual Configuration**

```bash
gram install claude-code
--mcp-url https://mcp.getgram.ai/org/project/environment
--api-key your-api-key
--header-name Custom-Auth-Header
--env-var MY_API_KEY
```

- Supports custom MCP URLs for non-Gram servers
- Configurable authentication headers
- Environment variable substitution for secure API key storage
- Automatic detection of locally set environment variables (uses actual value if available)
