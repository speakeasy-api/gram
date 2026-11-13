---
"server": minor
"dashboard": minor
---

Assistants no longer offer a "Set up agent identity" button, and the `assistants.upgradeIdentity` endpoint and the Platform MCP `upgrade_assistant_workload_identity` tool are removed. Speakeasy now moves each organization's assistants to their own agent identity: every assistant without one gets a dedicated agent, owned by the person who created the assistant, and its turns then run as that agent. The new agent can use every MCP server and skill in the assistant's project, and project inference when its owner can. An assistant whose creator has left the organization keeps running as before until it has a new owner. The organization's audit log shows these changes as made by the Speakeasy team.
