---
"server": patch
"dashboard": patch
---

Add a catch-all "Other agent" option to the Platform MCP connect flow, for agents outside the certified set. `client_family` accepts `other`, so an install on an uncertified agent is recorded as itself rather than mislabelled as a certified agent or left untracked.

No reviewed plugin package is built for such an agent, so both packaged install routes are closed for it and the walkthrough offers the remote MCP configuration alone. The install-method step lists only that route instead of showing the packaged ones greyed out, which read as an organization problem rather than what it is.

The agent list on the headless connect page also sets its own text color, so the marks drawn in `currentColor` — Cursor, Codex, opencode, and the new catch-all globe — no longer inherit the ink foreground and disappear into the dark panel.
