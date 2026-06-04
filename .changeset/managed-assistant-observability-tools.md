---
"server": minor
---

Grant the project's managed assistant (the dashboard Project Assistant) the full observability tool catalog the old client-side AI Insights copilot had. Previously it could only `search_logs`; it now also has `search_tool_calls`, `search_chats`, `search_users`, `get_project_metrics_summary`, `get_user_metrics_summary`, `get_observability_overview`, and `list_attribute_keys`. All wrap the existing telemetry service and are scoped to the managed assistant's platform toolset, so other assistants are unaffected.
