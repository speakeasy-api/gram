---
"dashboard": minor
---

An MCP Authentication Provider's Connections tab, under Identity > MCP Sessions, has a Register agent button for agents that cannot register themselves. It asks for a name, redirect URIs and the token endpoint auth method, then shows the minted client ID and secret once. The tab now also lists the provider's registered agents before they have signed in. A registration's redirect URIs can be edited from its detail sheet, except for CIMD agents, whose metadata document sets them. Both are shown only to people who can manage the provider's agents.
