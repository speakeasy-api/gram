---
"server": patch
---

Generated plugins now use the organization's stored default platform host, the host the dashboard shows, for MCP server and gateway addresses, the observability plugin's server URL, and the Codex telemetry endpoint. Organizations without a stored platform host keep the configured server URL, and custom-domain and private-network addresses are unchanged.
