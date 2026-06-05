---
"server": minor
---

Complete the managed assistant's observability catalog with the cross-service tools the old AI Insights copilot had: `get_deployment_logs` (deployments), `list_chats`/`load_chat` (chat), `list_organization_users` (organizations), and `list_risk_policies`/`list_risk_results_for_agent`/`list_risk_results_by_chat`/`get_risk_policy_status` (risk). Each wraps the existing management-service method and enforces that service's own authz against the calling user's grants — dashboard turns are sender-scoped, so e.g. the risk tools resolve only for org admins, exactly as the old copilot behaved. Granted only to the managed assistant's platform toolset.
