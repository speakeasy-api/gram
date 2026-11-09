---
"server": patch
---

The Platform MCP plugin now ships a `platform-sign-in` skill. When the Platform tools are missing or a call fails with an authentication error, the agent walks the user through signing in for their client, including `codex mcp login platform` for Codex, which does not open the sign-in page for plugin MCP servers on its own. The other Platform workflows now point to this skill instead of asking the user to repair OAuth, and the marketplace README lists the Codex sign-in command.
