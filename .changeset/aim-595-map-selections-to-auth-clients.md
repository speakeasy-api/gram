---
"server": patch
"admin": patch
---

The upstream OAuth migration now names, on every live credential's choice for a server, the one typed auth client that serves it, and reports coverage by credential class (interactive, ID-JAG, dashboard-minted, workload and agent sessions). A choice that several auth clients could serve is listed for an administrator to resolve instead of being guessed, and choices whose client is gone or no longer fits the server are listed as needing a reconnect.
