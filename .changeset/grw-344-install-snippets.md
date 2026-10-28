---
"server": patch
---

Platform MCP `get_my_install_instructions` now returns a ready-to-use path for each client when installing a single MCP server: a one-click Cursor install link plus its `mcp.json` block, the `claude mcp add` command for Claude Code, the `config.toml` block and `codex mcp login` command for Codex, the `opencode.json` block for OpenCode, and a generic JSON config for other clients. No credentials are included; sign-in still happens in the browser.
