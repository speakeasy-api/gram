---
"server": patch
---

When an ID-JAG sign-in to a single MCP server is refused because the person's account for it isn't connected or its connection has expired, the answer now names the server (or the credential the app chose) and links its connect page in `error_uri` and in the description, and connection logs record it as the person's to fix. Refusals that only an administrator can fix now say so in plain words, in ID-JAG and workload grants alike.
