---
"server": patch
---

An agent gateway now tells an agent what is wrong with a server's credential and who can fix it, instead of telling it to ask a gateway administrator. A missing, rejected or expired connection says the server needs re-authentication and asks someone who can edit the agent to open its MCP sign-in step (`/<org>/projects/<project>/identities/agents/<agent>/edit?step=2`) and reconnect it. A 403 from the upstream says the upstream account may not do the call, which signing in again does not change. An agent not allowed to use a connection is sent to its permissions page instead of being told to reconnect. A missing or rejected credential configured on the server, such as an API key, goes to an admin of the server's project. `connection_status` gives the sign-in link in its text and as `reconnect.url` in its structured result. No message names a person, and the links carry no credential.
