---
"server": minor
---

Round out the project's managed assistant (the dashboard Project Assistant) with the remaining AI Insights tools: it can now list and load chats (`platform_list_chats`, `platform_load_chat`), enumerate the organization's user directory (`platform_list_organization_users`), summarize risk findings without exposing secret content (`platform_list_risk_policies`, `platform_list_risk_results_for_agent`, `platform_list_risk_results_by_chat`, `platform_get_risk_policy_status`), and fetch deployment logs (`platform_get_deployment_logs`). Scoped to the managed assistant's platform toolset, so other assistants are unaffected.
