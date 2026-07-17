---
"server": patch
---

Allow `user_accounts.external_account_uuid` to be NULL and add a partial unique index on `(organization_id, provider, email)` for rows without a provider account UUID. This prepares the schema for persisting company-credential AI accounts (API key / gateway / Bedrock / Vertex sessions, which carry no `user.account_uuid`) keyed by their session email instead of the provider account UUID. No behavior change: ingest still only persists UUID-bearing accounts, and a temporary sqlc type pin keeps the generated Go type `string` until the follow-up feature change adapts call sites.
