---
"server": patch
---

A server connection check on the connect page that fails before reaching the service now records a connection event with its reason and who can fix it. Its error page names the check, suggests retrying only for an outage, and no longer shows internal error text. Any unexpected error page now says we're looking into it instead of suggesting a retry.
