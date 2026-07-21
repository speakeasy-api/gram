---
"dashboard": patch
---

Only offer enabled blocking Shadow MCP policies in the allow rule dialog. Previously the policy picker (and its default selection) included flag-action and disabled policies, which the server rejects with "policy must be an enabled blocking shadow mcp policy", and the resulting error was hidden behind a generic toast. The picker now filters to eligible policies on both the Shadow MCP inventory page and the server detail page, shows a hint when no eligible policy exists, and failed allow-rule updates surface the server's error message in the toast.
