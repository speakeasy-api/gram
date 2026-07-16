---
"server": patch
---

Classify Claude sessions authenticated by company credentials (an API key, gateway/proxy, Bedrock, or Vertex) as `team` for the account-type cost breakdown. These sessions emit no `user.account_uuid` (only a personal Claude subscription, which signs in via OAuth, does), so account attribution previously no-op'd and their entire spend fell into the `(unset)` bucket. Attribution now always classifies and stamps `account_type`; the `user_accounts` entity and device bridge, which key on the provider account UUID, are still skipped when it is absent.
