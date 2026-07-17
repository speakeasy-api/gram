---
"server": patch
---

Allow `user_accounts.external_account_uuid` to be NULL and add entity keys for rows without a provider account UUID: `(organization_id, provider, user_id)` for accounts resolved to an employee, and `(organization_id, provider, email)` as the fallback key while the employee is unresolved. This prepares the schema for persisting company-credential AI accounts (API key / gateway / Bedrock / Vertex sessions, which carry no `user.account_uuid`) — one account per employee, with email-keyed rows covering unprovisioned employees. No behavior change: ingest still only persists UUID-bearing accounts, and a temporary sqlc type pin keeps the generated Go type `string` until the follow-up feature change adapts call sites.
