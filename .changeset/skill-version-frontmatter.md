---
"server": patch
---

Skill version responses now include a `frontmatter` field with every top-level field parsed from the SKILL.md manifest, so spec fields like `license` and tool-specific extensions like `argument-hint` are visible without re-parsing the raw content.
