---
"dashboard": patch
---

An agent's servers and their sign-in are now changed on a new MCP sign-in tab on the agent's page, instead of by walking the six-step setup wizard again. The tab opens on the sign-in view, with Change servers for the server picker and Save changes that saves in place. The Edit button is gone from the agent page and the agents list; the agent is renamed inline from its page title. The expired-connection "Review connections" link and Permissions' "Change servers" open the new tab, old edit links redirect there, and a platform rule on the Agent sign-in tab has a Change link to the setup flow's Agent sign-in step. The Activity tab is renamed Audit trail. Links to an agent's renamed sections (for example `/activity` or `/provisioning`) now stay on that agent instead of landing on the Identities list. Command K offers each editable agent's MCP sign-in tab.
