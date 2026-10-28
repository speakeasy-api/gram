---
"server": patch
---

Adds the `legacy-alias-endpoints` operator backfill, which gives each hosted MCP server an explicit endpoint for every address its legacy toolset alias still serves, so stored client URLs keep working once the alias is removed.

The backfill keeps those addresses' slugs even without the organization slug prefix. Updating an MCP endpoint checks the prefix only when its address changes, so an existing unprefixed endpoint can still be saved.
