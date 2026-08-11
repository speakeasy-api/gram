---
"hooks": patch
---

Report whether the agent's MCP server list could actually be read. An agent with
no MCP servers and an agent whose list could not be read both produce an empty
snapshot, and the server denies MCP calls it cannot clear against one — so
without this the two are indistinguishable and a missing binary silently becomes
"this session has no servers". A partial or unparseable probe now relays the
entries it managed to see without claiming the listing is complete.
