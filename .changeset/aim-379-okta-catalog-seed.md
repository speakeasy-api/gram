---
"server": patch
---

Adds the `registry-okta-seed` command that creates or updates the Gram-owned catalog entries mapping Okta Integration Network applications to their MCP servers, for nine vendors. It is idempotent. On an existing entry it sets the Okta namespace and fills the icon and OAuth registration flag only when they are missing, leaving everything else untouched. `--dry-run` reports what it would change without writing. The local seed runs it so suggestions can be exercised in development.
