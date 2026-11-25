---
"server": minor
"dashboard": minor
---

Server connections on the Cross App Access page now show, on a line under the server, how many people your identity provider or the service refused in the last day who haven't connected since, when it last happened, what to check, and that Connection Logs list who was refused. Refused refreshes count. Setup problems and expired sign-ins don't. The count starts when Connection Logs were last turned on, if that was within the day. The server's status does not change. The Platform MCP `get_xaa_readiness` tool reports the same as `recent_refusals`, also for servers Speakeasy vouches for and in organizations without an Okta connection.
