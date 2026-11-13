---
"server": patch
---

Build organization encryption in the MCP, private ingress and admin processes, and refuse to start any process that handles OAuth credentials without it outside local and test environments. Set `GRAM_ORG_ENCRYPTION_DISABLED` to run without it.
