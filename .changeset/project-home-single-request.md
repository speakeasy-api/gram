---
"server": minor
"dashboard": patch
---

Speed up the project home page by cutting the requests it makes.

- `telemetry.getProjectOverview` takes a new `include_usage` flag. It returns the per-user and per-agent usage breakdowns in the same response. When the project has no agent usage, it returns `mcp_usage` instead: the number of external end users, the top end users by tool calls, the most used tools, and the tools with the highest failure rate. The dashboard no longer chains `telemetry.query` and `telemetry.searchUsers` calls after the overview.
- `telemetry.getProjectOverview` no longer returns fields the dashboard never read: `comparison`, `metrics_mode`, and the summary's `resolved_chats`, `failed_chats`, `active_users_count`, `top_users` and `llm_client_breakdown`. The server no longer runs the queries behind them.
- `chat.list` takes a new `pinned_first` flag. The home page's recent chats now load with one call instead of two.
- The dashboard asks the server to create the project assistant at most once per page load, instead of once per component that needs it.
