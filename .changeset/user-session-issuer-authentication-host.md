---
"server": minor
"dashboard": minor
---

Organization admins can move a Speakeasy authorization server onto or off the deployment's authentication host from its page, after previewing where clients will sign in. A shared authorization server with a pinned issuer URL is re-pinned to the new host, so its issuer URL and token endpoint follow. The new `organizationUserSessionIssuers.getAuthenticationHostImpact` and `setAuthenticationHost` methods back this, and issuers now report whether the deployment has an authentication host.
