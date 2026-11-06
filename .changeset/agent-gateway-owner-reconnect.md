---
"server": patch
---

An agent whose delegated upstream connection stops working is now told how to get it fixed. When the connection is missing, rejected by the upstream, or can no longer be refreshed, tool errors on the agent gateway say the server needs re-authentication and ask the agent's owner, by name, to open the agent's MCP sign-in step (`/<org>/projects/<project>/identities/agents/<agent>/edit?step=2`) and reconnect it. `connection_status` gives the same owner and link, in its text and as `owner` in its structured result, instead of telling the agent to ask a gateway administrator. The link carries no credential. It opens the dashboard, where the owner signs in and refreshes their own connection; anyone else must be allowed to edit the agent to assign another connection.
