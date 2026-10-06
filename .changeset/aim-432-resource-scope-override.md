---
"server": patch
---

Add `scope_override` to `remote_protected_resources`, the per-server operator pin for the scopes a login requests, and `omit_scope_fallback` to `remote_session_issuers`, the org-admin setting to send no `scope` instead of the authorization server's whole list. Nothing writes or reads either yet.
