---
"server": patch
---

A risk policy evaluate grant that names a server (`server_url` or `server_identity`) is now refused when saved. A policy's audience is the same on every server, so such a grant applied everywhere despite naming one server. Bypass and block grants still take a server.
