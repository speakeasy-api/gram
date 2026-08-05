---
"server": patch
---

Restore the Claude CLI on PATH before running the hooks binary, so MCP tool
calls stop being reported as shadow MCP on desktop and MDM-launched machines.
The binary shells out to `claude mcp list` to identify which MCP server a tool
call reached — the only source for connectors configured in claude.ai, which
appear in no local config file — and that lookup is a bare PATH search that
fails silently. A session launched from a GUI or by MDM routinely hands hooks a
minimal PATH without the CLI, so the lookup returned nothing and every MCP call
from that machine was reported with no server identity, which classifies as
shadow MCP. The bootstrap now probes the documented install locations, matching
what the Codex install script already does for `codex`.
