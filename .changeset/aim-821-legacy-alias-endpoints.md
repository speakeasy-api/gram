---
"server": patch
"dashboard": patch
---

Adds the `legacy-alias-endpoints` operator backfill, which gives each hosted MCP server an explicit endpoint for every address its legacy toolset alias still serves, so stored client URLs keep working once the alias is removed. The backfill keeps those addresses' slugs even when they lack the organization slug prefix or exceed the 40-character hosted slug limit. Updating an MCP endpoint checks the prefix only when its address changes, so an existing unprefixed endpoint can still be saved. The dashboard's address editor shows such a slug as it is stored and adds the prefix only when the address is renamed.

A hosted MCP server that has an endpoint on the organization's live custom domain now shows that custom-domain URL in its toolset view, its exported MCP metadata and assistant connections, the same URL plugins already use. Before, these showed the platform URL when the server also had one. The dashboard still lists a domain-root server's address in its `/mcp/<slug>` form, which also serves.
