---
"server": patch
---

Connecting to an identity provider that shows its own error page when it refuses the RFC 8707 `resource` parameter, instead of redirecting back with `invalid_target`, no longer strands the user. If a login that sent the resource never came back, restarting it within 10 minutes retries once without the resource, the same retry an `invalid_target` redirect gets. A new `gram.remote_session.upstream_authorize.unanswered` counter, labelled by issuer only, counts logins the provider never answered.
