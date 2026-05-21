---
"server": minor
---

Add a nullable `scope` column to `remote_session_clients` and surface it on the remoteSessionClients management API. When set, the upstream OAuth dance requests these scopes instead of echoing the issuer's full `scopes_supported`, which avoids over-granting Gram access on providers that advertise broad scope sets.
