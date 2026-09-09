---
"server": minor
---

Remote logins now request `openid`, `email`, `profile`, and `offline_access` whenever the upstream issuer advertises them, on top of the client's stored scope or the issuer's `scopes_supported`; operators can pin a verbatim request per issuer with the new `scope_override`. A login or token refresh the upstream answers with `invalid_target` is retried once without the RFC 8707 `resource` parameter, while the resource stays recorded on the grant; operators can set `resource_indicator_supported` to false on an issuer that never accepts it. Issuers that advertise the RFC 9207 `iss` parameter have it validated on the callback, and the consent page offers a reconnect when a live grant lacks `openid` that a reconnect would now request.
