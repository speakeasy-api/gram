---
"server": minor
---

Grant the project's managed assistant (the dashboard Project Assistant) the full observability and AI Insights tool catalog the old client-side copilot had. It can now search and inspect activity (`search_logs`, `search_tool_calls`, `search_chats`, `search_users`), pull project- and user-level metrics and overviews (`get_project_metrics_summary`, `get_user_metrics_summary`, `get_observability_overview`, `list_attribute_keys`), list and load chats (`platform_list_chats`, `platform_load_chat`), enumerate the organization's user directory (`platform_list_organization_users`), summarize risk findings without exposing secret content (`platform_list_risk_policies`, `platform_list_risk_results_for_agent`, `platform_list_risk_results_by_chat`, `platform_get_risk_policy_status`), and fetch deployment logs (`platform_get_deployment_logs`). Scoped to the managed assistant's platform toolset, so other assistants are unaffected.
