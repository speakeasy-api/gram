---
"server": patch
---

Adding a hosted MCP server to a gateway now wires the OAuth provider its external MCP tools authenticate against: the gateway registers a client with that provider (reusing one the project already has), offers it on the gateway's connect page, and forwards the user's token to the member's tools. The hosted server's own endpoint is unchanged. Previously such members listed and described fine but every call was refused upstream with an opaque error. The add is refused with an explanation when the gateway could route no single credential to the member: several OAuth upstreams in one toolset, OpenAPI or function tools that also take OAuth, another member on the same provider, or a provider without client registration. A tool call an external MCP server rejects for missing or expired credentials now says so instead of reporting a generic failure.
