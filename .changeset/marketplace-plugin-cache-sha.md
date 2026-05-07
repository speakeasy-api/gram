---
"server": patch
---

Fix private Claude Code plugins showing "not cached at (not recorded)" after restarting Claude Code. The marketplace proxy now fetches the current HEAD commit SHA and embeds it alongside `ref` in each `git-subdir` plugin source, giving Claude Code a stable cache key that survives restarts.
