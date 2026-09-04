---
"server": patch
---

Add nullable columns to `remote_sessions` for the identity an upstream grant belongs to (`upstream_subject`, `upstream_email`, `upstream_display_name`, `identity_source`, `enrichment`) and for observed token validity (`last_validated_at`, `validation_status`, `validation_reason`). Schema only; nothing writes them yet.
