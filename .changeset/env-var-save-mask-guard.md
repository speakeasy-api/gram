---
"dashboard": patch
---

Fix the MCP server Authentication tab persisting the server-redacted placeholder (e.g. `sup*****`) as the real environment variable value. Saving now only writes a value the user actually typed, removes on an intentional clear, and otherwise leaves the stored secret untouched — covering both the state-toggle path and untouched saves that swept in unmapped required variables.
