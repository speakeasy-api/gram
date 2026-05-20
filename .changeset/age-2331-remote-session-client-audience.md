---
"server": minor
---

Add a nullable `audience` column to `remote_session_clients` and surface it on the remoteSessionClients management API. When set, the upstream OAuth dance attaches the `audience` parameter to the authorize redirect, the authorization-code → token exchange, and every refresh-token request; when unset the parameter is omitted entirely.
