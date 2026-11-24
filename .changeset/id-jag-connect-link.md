---
"server": patch
---

When an ID-JAG sign-in to a single MCP server is refused because the person's account for it isn't connected or its connection has expired, the answer now names the server (and the chosen credential, when the app named one) and links its connect page in `error_uri` and in the description. ID-JAG, workload and assistant grants that hit this refusal now record it in connection logs as the person's connection being missing, for the person to fix, and their admin-side refusals use plain wording.
