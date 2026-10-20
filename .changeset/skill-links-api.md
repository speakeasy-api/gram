---
"server": minor
---

Add a skill links management API: `skills.link`, `skills.unlink` and `skills.listLinks` choose which skills an MCP server, a gateway or the organization gateway serves over MCP. Linking is idempotent per skill and target, a repeat only changes the version pin, unlinking a link that is already gone is a no-op, and each change is audit-logged. Writes need skill read plus MCP write on the server (project write for gateways), listing only shows links to servers the caller can read, and all three methods are refused unless serving skills over MCP is switched on for the organization. The Platform MCP gains `link_skill_to_mcp` and `unlink_skill_from_mcp`, and `list_skill_distributions` now reports the MCP surfaces that serve each skill under `served_by`.
