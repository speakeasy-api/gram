---
"server": minor
---

Remote logins now request `openid`, `email`, `profile`, and `offline_access` whenever the upstream issuer advertises them, on top of the client's stored scope or the issuer's `scopes_supported`; operators can pin a verbatim request per issuer with the new `scope_override`. An issuer that rejects the RFC 8707 `resource` parameter with `invalid_target` is recorded as such (`resource_indicator_supported`) and the login is retried once without it, while the resource stays recorded on the grant. Issuers that advertise the RFC 9207 `iss` parameter have it validated on the callback, and the consent page offers a reconnect when a live grant lacks `openid` that a reconnect would now request.
