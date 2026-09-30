---
"server": patch
---

Adds the `registry-okta-seed` command that creates or updates the Gram-owned catalog entries mapping Okta Integration Network applications to their MCP servers, starting with Linear. It is idempotent, only ever writes the Okta namespace on an existing entry, and `--dry-run` reports what it would change without writing. The local seed runs it so suggestions can be exercised in development.
