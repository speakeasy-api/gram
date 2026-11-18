---
"server": patch
---

Gateway members and connection checks now resolve credentials from each member's own clients. A user session issuer that binds several clients for one provider, such as after consolidating issuers, no longer fails every connection check behind the gateway. Where upstream auth is enforced, a check of a typed auth client's card uses that client's registration, the same one requests are served from.
