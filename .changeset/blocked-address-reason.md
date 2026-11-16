---
"server": patch
---

Add two MCP connection reason codes, `address_not_allowed` and `address_not_found`, fixed by an admin. They appear in connection rows and in the connection log export (`error.type`) when a server's or sign-in provider's address is one Speakeasy does not connect to directly, or a name that does not exist. Rows keep the configured host, except an HTTP tool's client credentials rows, which carry none. Failures recorded until now as `provider_unreachable` or `remote_session_unavailable` (and `credential_misconfigured` for an HTTP tool's blocked token URL) move to these codes, so provider-owned failures drop and admin-owned failures rise at deploy. Error text, the sign-in error page, the connect page, Code Mode and connection status now say the address is the problem and who checks it, instead of asking people to retry. Status codes, error types and retry behaviour are unchanged.
