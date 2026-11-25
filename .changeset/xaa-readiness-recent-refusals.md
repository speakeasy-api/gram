---
"server": minor
"dashboard": minor
---

Server connections on the Cross App Access page now show, on a line under the server, how many people your identity provider or the service refused in the last day, when it last happened, what to check, and that Connection Logs for the server, or for the gateway people used, list who was refused. Refused refreshes count too. Setup problems and expired sign-ins are not counted. The server's status does not change. The Platform MCP `get_xaa_readiness` tool reports the same as `recent_refusals`, also for servers Speakeasy vouches for, including in organizations without an Okta connection.
