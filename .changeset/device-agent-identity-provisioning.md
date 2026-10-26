---
"dashboard": minor
"server": minor
---

Agents can now be provisioned to run the device agent. In the new agent wizard, choose **Device agent** on the Credential step and copy a one-line install command for an ephemeral or persistent Linux host, with no email, version, or checksum. The agent gets only plugin sync, hook ingestion, and read access to its project, never MCP access, and Speakeasy will not generate a device agent install command for a key that can reach MCP servers.
