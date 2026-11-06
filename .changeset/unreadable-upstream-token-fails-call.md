---
"server": patch
---

When Speakeasy can't read a person's stored upstream access token, the call now fails as a Speakeasy error that names Speakeasy support. Gateways no longer call the member without credentials, direct servers no longer ask the person to reconnect, and the connection is recorded as `internal_error`.
