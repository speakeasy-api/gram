---
"server": patch
---

Stop fail-closing allow-all shadow-MCP policies when a session has no MCP
inventory snapshot. An allow-all policy denies only on a blocked-URL match,
and a missing inventory yields no URL to match — the same reasoning that lets
unresolvable evidence fall through to an allow in the canonical guard. Before
this, one failed SessionStart gather (typically a GUI-launched hook process
without the `claude` CLI on PATH) blocked every MCP call for the whole session
even under a permit-by-default policy.

Block-all policies still fail closed, but the denial now logs at WARN under
`claude_hook_denied_no_mcp_list` so a monitor can catch sessions bricked in
this state, and the user-facing message names the real cause (the hooks
client never reported MCP configuration) and the real remediations instead of
suggesting a retry or restart that cannot help when the outage is chronic.
