---
"server": minor
"hooks": minor
---

Hooks ingest responses now carry a `hooks_version` effect saying whether the sender's hooks binary is current, behind or unsupported. The hooks binary shows a once-per-session notice in Claude Code and Codex when its version is unsupported. The retired Claude hooks endpoint shows the same kind of notice when a session starts. Allow and deny decisions are unchanged.
