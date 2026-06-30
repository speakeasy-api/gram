---
"dashboard": patch
---

Surface each employee's linked AI accounts across the Employees insights pages. The list page gets a clickable accounts column (popover listing email, provider, and team/personal type) plus an account type filter. The employee detail page gets an "AI Accounts" card and an account scope selector next to the date range: the default is the cumulative all-accounts view, and selecting a single account re-scopes the entire page (tokens, cost, tool calls, platform breakdown, top tools, timeseries, data flow) to that one account. Supports multiple accounts per employee across providers (Claude, Codex, Cursor), including the same email on different providers.
