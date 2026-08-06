---
"server": patch
---

Fix a credential leak in published plugin configs: an unproxied MCP server (one whose URL points directly at a vendor, never through the platform's own gateway) could have the org's API key attached as a static Authorization header, sending it straight to the third-party vendor's server instead of the platform. Unproxied servers now carry no Gram-managed credential in any generated client config (Claude, Cursor, Codex, OpenCode), and no longer trigger an unnecessary API-key prompt during install.
