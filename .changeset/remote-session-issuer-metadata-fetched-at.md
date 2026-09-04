---
"server": patch
---

Add nullable `metadata_fetched_at`, `metadata_last_error`, and `metadata_last_error_at` columns to `remote_session_issuers` so a stale issuer capability set can be told from a fresh one and a failed refresh leaves its reason. Schema only; nothing writes them yet.
