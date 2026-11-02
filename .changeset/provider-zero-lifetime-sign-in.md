---
"server": patch
---

A sign-in where the provider reports a zero, negative or unreadable lifetime now works instead of showing as expired straight away or failing. "Give access" can no longer issue a token while no connection can serve the server, and the page says to connect or reconnect a service. The verify log now says why a connection had no usable credential.
