---
"server": patch
---

Add nullable columns to `remote_sessions` for the identity an upstream grant belongs to (`upstream_subject`, `upstream_email`, `upstream_email_verified`, `upstream_display_name`, `upstream_picture_url`, `upstream_session_id`, `upstream_auth_time`, `identity_source`, `identity_verified_at`, `enrichment`) and for observed token validity (`last_validated_at`, `validation_status`, `validation_reason`). Schema only; nothing writes them yet.
