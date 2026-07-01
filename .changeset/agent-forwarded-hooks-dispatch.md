---
"server": minor
---

Add `hooks.dispatch`: an agent-forwarded hook endpoint authenticated with the device agent's org-scoped key (not a hooks-scoped key, which the agent never holds). It resolves the org's default project — with an optional `project_slug` override — vouches for the forwarded `user_email`, and fans out to the per-tool Cursor/Claude/Codex hook handling, returning a normalized decision. This lets the device agent proxy hook events on every OS without baking a hooks key into a script (DNO-376).
