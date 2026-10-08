---
"server": minor
"dashboard": minor
---

A tunnel can now front several MCP servers from the dashboard: the tunneled create page offers **Existing tunnel**, which adds an MCP server to a tunnel the project already has without issuing a new key. A tunnel-backed server's settings now separate **Delete this MCP server**, which leaves the tunnel and its other servers running, from **Delete tunnel and its MCP servers**, which deletes only the servers the user reviewed. Tunnel-wide settings (key rotation, resource identifier, public access, public rate limit, agent setup) list the MCP servers they affect and warn that public servers on the tunnel bypass per-tool access control.

`tunneledMcp.deleteServer` now returns a conflict while any MCP server still uses the tunnel, instead of deleting the tunnel underneath them. Delete the MCP servers first. Deleting a tunnel that does not exist or is already deleted still succeeds.
