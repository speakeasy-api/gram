---
"server": minor
---

Extend the Platform MCP `add-existing-mcp-servers` workflow so it works from any agent client, reports local (stdio or localhost) servers and leaves them unchanged, and can optionally move migrated servers onto the organization's Tailscale private network.

Add a read-only `get_network_ingress` Platform MCP tool for organization administrators. It reports whether private networking is switched on for the organization, the ingress's configured, enabled and observed state, and the one next action standing between the organization and private MCP access, with a dashboard setup link. It never returns provider credentials or resources.
