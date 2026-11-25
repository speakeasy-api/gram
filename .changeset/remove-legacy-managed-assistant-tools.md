---
"server": patch
---

Remove the legacy managed-assistant platform toolset and the `assistant-platform-mcp` rollout flag. The project's managed assistant now always gets the Platform MCP read toolset, which every organization already used, and its system prompt now names the Platform MCP tools instead of the removed `platform_*` tools. The changelog tool has no Platform MCP counterpart yet, so the prompt no longer mentions it. The Speakeasy Slack toolset and the base assistant toolset (memory, skills, triggers) are unchanged.
