---
"server": patch
---

Platform MCP now speaks to administrators rather than to its own internals. The server instructions carry a voice contract — what to explain, what never to say aloud, and how to keep "connect", "add to a plugin", and OAuth's own "register" from being confused with each other — and `get_platform_context` returns a plain-language account of how an MCP server reaches a person. Every tool title and description, every message handed back in a tool result, and the repair-plan labels were rewritten to lead with what changed for the administrator's people and keep mechanism behind an explicit `Constraints:` note. The `AICP` acronym is gone from the tool surface, and the fault attribution for a misbehaving caller reports `mcp_client` rather than `client`, so it cannot be read as the OAuth client.
