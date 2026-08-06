---
"hooks": patch
---

Make Claude MCP inventory collection failures diagnosable. The relay's
`claude mcp list` gather at SessionStart bailed silently when the CLI was not
on PATH or the command failed — and a session whose gather fails carries no
inventory for its whole life, which under a block-all shadow-MCP policy
blocks every MCP call in the session (DNO-784). Each bail-out now writes the
reason (including captured stderr) to the relay debug log, and
`GRAM_HOOKS_CLAUDE_BIN` is honored as an explicit CLI location, matching the
plugin bootstrap's documented override for machines where the CLI is not
discoverable via PATH.
