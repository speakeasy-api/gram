---
"dashboard": patch
---

The agent wizard's workload setup now picks its token endpoint from the agent's MCP servers. It offers only the user session issuers those servers sign in through and preselects the one serving most of them, where it used to preselect the organization's first shared issuer, whose tokens the servers refused. Servers a token from the selected issuer won't work for are listed with why and how to fix it: a server on another shared issuer has a button to use that issuer, and a server whose own sign-in can't give tokens to agents can have the shared sign-in assigned to it from the wizard, after a confirmation that previews the change. Assigning is offered only when nobody connects their own account through the server's current sign-in and the reader can edit the server; otherwise the wizard links to its sign-in settings. The exchange step's example call offers only servers that accept the token.
