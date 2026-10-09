---
"server": minor
"dashboard": minor
---

Remote and tunneled MCP servers send upstream headers from their linked environment. Only entries named `MCP_HEADER_<Header-Name>` are sent; every other variable in the environment is ignored. An environment header replaces a source header for the same field, including one copied from the caller's request. It owns every spelling of its name, is stripped on cross-origin redirects, and survives tunnel retries. A signed-in user's upstream token still wins for `Authorization`. Speakeasy credentials, tunnel fields and MCP protocol headers are reserved. A request is refused, without an upstream call, when the linked environment is deleted or unavailable, or when an `MCP_HEADER_` entry cannot be sent (invalid or reserved name, empty or invalid value, duplicate, undecryptable). Settings gain an Environment Headers section and `mcpServers.getEnvironmentHeaders`, a names-only preview that needs MCP read access and project-wide environment read access.

Rollout: environment links on remote and tunneled MCP servers were previously inert. After this release, any `MCP_HEADER_` entries in an environment already linked to such a server start being sent upstream. To audit before deploy, list the entry names (not values) in environments linked to proxied MCP servers.
