---
"dashboard": minor
"server": minor
---

Agents can now be provisioned to run the device agent. In the new agent wizard, set **Used for** to **Device agent**, pick the project its hook events go to, and copy a one-line install command for an ephemeral or persistent Linux host, with no email, version, or checksum. The agent gets only plugin sync, hook ingestion, and read access to that project, never MCP access, and Gram will not generate a device agent install command for a key that can reach MCP servers. Agent API keys now list when each was last used.
