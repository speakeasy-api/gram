---
"dashboard": patch
---

Narrowing someone's connect access to read-only tools on an MCP server's Team Access tab now reads back as the read-only tools it grants, instead of showing no access. The page now matches each tool to the single annotation authorization checks it under, so an OpenAPI GET that is also idempotent and open-world counts as read-only, and the narrowing dialog counts each tool once.
