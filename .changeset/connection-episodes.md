---
"server": patch
---

Daily connection outcomes now credit the owner's next connection with fixing a refused, already used sign-in. Days stored more than about 11 days before this deploys keep the old count. Adds the read behind failing connection detection, not yet shown anywhere: it works out who cannot use an MCP server, gateway member or toolset right now from recorded connection attempts and served requests. A failure ends when the part that broke is next seen working: the app's own served request for its sign-in, the server answering the person for their service sign-in, and the server answering anyone for an outage. The caller chooses how recently someone must have tried.
