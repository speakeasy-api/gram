---
"server": patch
---

Restore the Claude CLI on PATH before running the hooks binary, so the MCP
inventory snapshot is collected on desktop and MDM-launched machines. The
relay shells out to `claude mcp list` at session start to build the snapshot
behind the Shadow MCP inventory page, and resolves the CLI with a bare PATH
search that fails silently. A session launched from a GUI or by MDM routinely
passes a minimal PATH without the CLI, so the snapshot was never collected and
the inventory stayed empty for that machine. The bootstrap now probes the
documented install locations, matching what the Codex install script already
does for `codex`.
