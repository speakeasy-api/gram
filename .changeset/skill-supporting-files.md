---
"server": minor
---

Skills can carry supporting files alongside SKILL.md, such as `scripts/`, `references/` and `assets/` entries. `skills.create`, `skills.addVersion` and `skills.approveSuggestion` accept a `files` list, every skill version reports its `files`, and the new `skills.getVersionFile` method returns one file's bytes. When `files` is omitted, a new version keeps the files of the version it builds on. The Platform MCP `create_skill`, `add_skill_version` and `approve_skill_suggestion` tools accept files too.
