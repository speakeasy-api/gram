---
"server": patch
---

fix: make Claude session user attribution deterministic. The hook-supplied device-enrolled employee email now always wins over the OTEL-cached account email (the AI account's own report, e.g. a personal gmail) when both are present — previously whichever ingest stream created the chat row first determined the session's `external_user_id`. The account's own email is unaffected and remains surfaced via `user_accounts` / `account_email`.
