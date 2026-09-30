---
"server": patch
"dashboard": patch
---

Gram now works end to end on extra platform hosts such as `ai.speakeasy.com`. Platform MCP advertises, issues, and accepts tokens for the host it is used on. The MCP install page login, the Stripe billing portal, and Polar checkout return to that host. Tunneled MCP agent setup shows the real tunnel gateway, the explore demo link stays on the current host, and the custom domain CNAME fallback points at the right target.
