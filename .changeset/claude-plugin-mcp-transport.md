---
"hooks": patch
---

Resolve the URL or command of Claude Code plugin MCP servers from the installed plugin's own config. Tool calls to plugin servers (`mcp__plugin_<plugin>_<server>__<tool>`) now carry the server's real transport, so Shadow MCP recognises Speakeasy-hosted plugin servers and stops flagging or blocking them, while third-party plugin servers keep their true URL or command.
