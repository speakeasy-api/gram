---
"dashboard": patch
---

The Shadow MCP page and server pages show progress and explain failures. Retry buttons spin while they work, gathering evidence and saving a server name show a spinner, and the decision button reads "Approving…" or "Blocking…". Failed loads show the reason under a plain title. Failed actions raise one toast with a plain title, the reason, and Retry where it helps, instead of a generic "Request failed" toast beside a second one with no reason. A successful rename is now confirmed.
