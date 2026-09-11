---
"server": minor
"dashboard": patch
---

Recover automatically from expired upstream dynamic client registrations. Remote session clients now record the registration endpoint they were registered at and the expiry the identity provider reported. When a provider stops recognizing a client (`invalid_client` on refresh), Gram confirms it against the token endpoint and re-registers the client in place before the next login, revoking the sessions bound to the old client. Organization administrators can also rotate a client on demand from its settings page.
