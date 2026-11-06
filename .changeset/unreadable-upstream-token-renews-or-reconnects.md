---
"server": patch
---

When Speakeasy can't read a person's saved upstream access token, it now renews the sign-in in the background with the saved refresh token, and the call goes through as normal. When nothing saved can be read, the person is asked to reconnect: direct servers send the usual sign-in challenge, and gateways ask the person to reconnect that server instead of calling it without credentials. The connection is recorded as `stored_sign_in_unreadable`.
