---
"server": patch
---

Adds the `legacy-alias-endpoints` operator backfill, which gives each hosted MCP server an explicit endpoint for every address its legacy toolset alias still serves, so stored client URLs keep working once the alias is removed.
