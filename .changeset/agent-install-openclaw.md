---
"server": minor
"dashboard": minor
---

The one-command agent install now sets up OpenClaw as well as Claude Code and Codex. On OpenClaw it adds the agent gateway as an MCP server, installs the Speakeasy observability plugin with the agent's key, allows the plugin conversation access, and finishes with a `Speakeasy hooks: PASS` or `FAIL` line that explains a missing grant, followed by a reminder to restart the OpenClaw Gateway, which loads plugins only at startup. The agent page offers a message to paste into OpenClaw's chat so OpenClaw runs the command itself. The OpenClaw observability plugin now declares the hook capability, which OpenClaw 2026.9.3 and later need to load it at startup, and plugin ZIP downloads mark the key-carrying `speakeasy.json` owner-only.
