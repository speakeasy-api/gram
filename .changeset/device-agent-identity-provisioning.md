---
"dashboard": minor
"server": minor
---

Agents can now be provisioned to run the device agent. In the new agent wizard, choose **Device agent**, pick the project its hook events go to, and copy a one-line install command for an ephemeral or persistent Linux host — no email, version, or checksum. The agent gets only plugin sync, hook ingestion, and read access to that project, never MCP access, and the install command refuses a key that can reach MCP servers. Agent API keys now list when each was last used.
