---
"dashboard": patch
"server": patch
---

A sign-in made through a team gateway now counts for each member server whose own sign-in issuer binds the same OAuth client. The agent credentials wizard shows those servers as connected, and attaching the connection to an agent succeeds. A connection for a client the server does not bind still does not count.
