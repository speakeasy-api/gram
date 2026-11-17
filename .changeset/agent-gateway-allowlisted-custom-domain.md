---
"server": patch
"dashboard": patch
---

Organizations whose custom domain has an IP allowlist can reach the agent gateway on that domain. The custom-domain ingress now routes `/agent-mcp`, the agent setup wizard and the one-line installer give agents `https://<custom-domain>/agent-mcp/<agent-id>`, and the federated access setup lists the custom domain as the MCP host. Other organizations keep the platform host.
