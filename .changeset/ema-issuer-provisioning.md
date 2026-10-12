---
"server": minor
"dashboard": patch
---

Organizations with single sign-on can enable an internal session issuer through the new `internalSessionIssuers` API (get, setEnabled, rotate): a permanent issuer URL with a dedicated Speakeasy-managed signing key, routine and compromise key rotation, and a disable switch that keeps the URL. A compromise rotation withdraws every key first, then replaces it and disables the old KMS versions, retrying cleanup hourly until it succeeds. Expired keys are retired hourly. Signing can be paused for the platform or one organization with the `internal-session-issuer-signing-paused` flag, which pauses signing unless it is explicitly off, or `GRAM_INTERNAL_SESSION_ISSUER_SIGNING_PAUSED`. Rollout is gated by the `internal-session-issuer` flag. The issuer's keys are never visible to customer key APIs or usable for client authentication. A read-only Platform MCP tool, `get_internal_session_issuer`, reports the issuer's state and URL.
