---
"server": minor
"dashboard": minor
---

Import skills from a public GitHub repository. The new `skills.fetchFromGitHub` endpoint scans a repository's default branch for SKILL.md manifests and returns parsed results pinned to the resolved commit, and the dashboard's add-skill dialog gains a GitHub import flow that lets you select and import discovered skills — including ones with spec validation warnings. Oversized SKILL.md files are reported as scan issues instead of being silently skipped, and repositories that are empty, too large, or contain too many manifests return clear validation errors.
