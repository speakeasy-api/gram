---
"server": minor
---

Extend plugin publishing to generate Codex-compatible packages alongside
Claude Code and Cursor. Each published plugin now also includes a
`.codex-plugin/plugin.json` manifest and `.mcp.json` server config, with a
top-level `.agents/plugins/marketplace.json` listing all plugins for
installation via `codex plugin marketplace add`.
