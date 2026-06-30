---
"dashboard": patch
---

Add a team vs personal account type filter to the AI Agent Costs (/insights agents) page. The selection scopes the observability overview, per-user, and per-role queries so the cost/token/usage breakdowns reflect only the chosen account type. The shared `ACCOUNT_TYPE_OPTIONS` constant is centralized in `observeFilterConstants`.
