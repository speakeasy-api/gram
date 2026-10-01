---
"server": patch
---

Seeds the Gram-owned catalog entries that map Okta Integration Network applications to their MCP servers, for 28 vendors. Workers apply the seed automatically when they start, once per version of the vendor table, so adding a vendor is a code change with no command to run. The seed is idempotent: on an existing entry it sets the Okta namespace and fills the icon and OAuth registration flag only when they are missing, leaving everything else untouched. The mechanism is generic, so other reference data can be seeded the same way, and `gram app-seed` applies every startup seed directly for a database no worker has seeded.
