---
"server": patch
---

Adds the `registry-okta-seed` command that creates or updates the Gram-owned catalog entries mapping Okta Integration Network applications to their MCP servers, for 28 vendors. It is idempotent. On an existing entry it sets the Okta namespace and fills the icon and OAuth registration flag only when they are missing, leaving everything else untouched. `--dry-run` reports what it would change without writing. Workers now apply it automatically when they start, once per version of the vendor table, so adding a vendor is a code change with no command to run. The command stays for dry runs and for the local seed.
