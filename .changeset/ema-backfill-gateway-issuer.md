---
"server": patch
---

The upstream auth client backfill now records an enterprise-managed authorization (EMA) client for a binding made for the organization's gateway sign-in issuer, and offers it on the servers in the binding's project at the binding's resource. Previously such a binding was reported as matching no server, so its client was never recorded. Requests are still chained only through a binding for their own sign-in issuer and project.
