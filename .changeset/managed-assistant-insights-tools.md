---
"server": minor
---

Round out the project's managed assistant (the dashboard Project Assistant) with the remaining AI Insights tools: it can now list and load chats (`list_chats`, `load_chat`), enumerate the organization's user directory (`list_organization_users`), summarize risk findings without exposing secret content (`list_risk_policies`, `list_risk_results_for_agent`, `list_risk_results_by_chat`, `get_risk_policy_status`), and fetch deployment logs (`get_deployment_logs`). Scoped to the managed assistant's platform toolset, so other assistants are unaffected.
