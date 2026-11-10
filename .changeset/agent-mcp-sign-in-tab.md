---
"dashboard": patch
"server": patch
---

An agent's servers and their sign-in are now changed on a new MCP sign-in tab, second on the agent's page after Overview, instead of by walking the six-step setup wizard again. The tab lists the servers and how Speakeasy signs in to each, and saves each change as it is made, with a toast: picking servers in the Change servers dialog saves on Continue, and removing a server or connecting an account saves at once. A red count on the tab shows how many of the agent's servers need someone to sign in. The Edit button is gone from the agent page and the agents list; the agent is renamed inline from its page title. The expired-connection "Review connections" link, Permissions' "Change servers", and the agent gateway's "reconnect it" message now link to the new tab, old edit links redirect there, and a platform rule on the Agent sign-in tab has a Change link to the setup flow's Agent sign-in step. The Activity tab is renamed Audit trail, and an agent's old Activity links land on it. Command K offers each editable agent's MCP sign-in tab.
