---
"server": patch
---

When Speakeasy can't read a person's saved upstream access token, it now renews the sign-in with the saved refresh token during the call, without the person noticing. When nothing saved can be read, the connection shows as not connected, so connecting again replaces it: direct servers send the usual sign-in challenge, and gateways ask the person to reconnect that server instead of calling it without credentials. These requests are recorded as `stored_sign_in_unreadable`.
