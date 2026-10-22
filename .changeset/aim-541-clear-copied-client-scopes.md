---
"server": patch
---

Add a `client-scopes` mode to the migrations tool. It clears the scope copied onto remote session clients created before automatic clients stopped storing one, so their logins discover what to request.
