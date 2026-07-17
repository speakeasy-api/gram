---
"server": minor
---

Skills can now be distributed to plugins. New management endpoints let you attach a skill to a plugin (tracking the latest valid version or pinning a specific one), revoke a distribution, and list a project's active distributions. Deleting a plugin or archiving a skill automatically revokes the affected distributions. Distributed skills ship inside the published plugin packages for Claude Code, Cursor, and Codex, and distribution changes mark the plugin as having unpublished changes so the next publish or marketplace auto-sync picks them up.
