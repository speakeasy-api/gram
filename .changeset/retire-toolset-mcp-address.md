---
"server": minor
---

Hosted MCP servers are now reached only through their endpoints. An older address that only the toolset still held, with no endpoint at that slug, stops serving: `/mcp/<slug>` requests, its sign-in metadata, its sign-in routes and its install page return not found. Access tokens must name the server they were issued for; tokens issued for the toolset before hosted servers had endpoints are rejected, and clients sign in again. To keep an address working, add it as an endpoint on the server. Plugins include a hosted server only when it has an endpoint, and adding a toolset to a plugin needs one.
