---
"server": patch
---

Classify Claude sessions authenticated by company credentials (an API key, gateway/proxy, Bedrock, or Vertex) as `team` for the account-type cost breakdown. These sessions emit no `user.account_uuid` (only a personal Claude subscription, which signs in via OAuth, does), so account attribution previously no-op'd and their entire spend fell into the `(unset)` bucket. Attribution now always classifies and stamps `account_type`, and these sessions also teach the device-owner bridge (keyed on the per-device id, not the account UUID) so a personal account later seen on the same device can be attributed to its employee; only the `user_accounts` entity and billing mode, which key on the absent UUID, are skipped.
