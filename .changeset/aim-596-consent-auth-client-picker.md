---
"server": minor
---

Where typed upstream authentication is enforced, the consent screen offers each MCP server's upstream credentials that the person may use and records the one they pick for the new connection. A server with several shows a choice, preselecting the credential the person last approved there, otherwise an identity-provider sign-in, then a shared credential by name, then their own account, then their app's own credential. A gateway offers the choice per server. A server whose credentials the person may not use reads "No access", and one whose sign-in isn't set up says so, instead of offering a connection that cannot work.
