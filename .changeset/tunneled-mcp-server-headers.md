---
"server": minor
"dashboard": minor
---

Tunneled MCP servers can now carry configured upstream headers, like remote MCP servers. The new `tunneledMcp` list, get, create, update and delete header endpoints store secret values encrypted and audit every change. The tunnel adds the headers to every request it forwards to the upstream server, and keeps them when it retries on another gateway. Names are stored in canonical form and compared case-insensitively. Tunneled sources reject Speakeasy credential, tunnel, MCP protocol, hop-by-hop and cookie headers, either as a header name or as the source of a passed-through value, and never forward Speakeasy credential headers from the caller's request. Remote MCP server header behavior is unchanged. MCP server settings for a tunneled source gain an Upstream Headers section, which notes that the headers apply to every MCP server on the tunnel.
