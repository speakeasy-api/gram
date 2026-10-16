---
"server": patch
---

The platform hosts now answer `/.well-known/mcp-registry-auth` with the proof record set in `GRAM_MCP_REGISTRY_AUTH`, so the Speakeasy Platform MCP can be published to the official MCP registry as `com.speakeasy.ai/platform`. Without the record, these hosts return not found. Custom domains never serve it.
