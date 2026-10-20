---
"server": patch
---

Skills and skill distributions now report `last_loaded_over_mcp_at`, the last time an agent loaded the skill over a Speakeasy-hosted MCP server. Skill list and get return it, so a skill with no plugin distribution still reports its loads. The Platform MCP `list_skills`, `get_skill` and `list_skill_distributions` tools return the same value.
