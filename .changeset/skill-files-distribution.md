---
"server": minor
"dashboard": patch
---

Skills with supporting files are distributed whole: plugin packages, the MCP Skills extension, shared skill pages and the assistant skills load tool now carry every file of the skill, not only SKILL.md. `skills.getShared` lists a shared skill's `files` and an `archive_path`, and the new `skills.downloadShared` method returns the whole skill directory as a ZIP.
