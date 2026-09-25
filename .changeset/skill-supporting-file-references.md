---
"server": minor
"dashboard": minor
---

Report the supporting files a SKILL.md depends on. The Agent Skills specification defines a skill as a directory — `SKILL.md` plus optional `scripts/`, `references/`, and `assets/` — while Gram stores and distributes only the manifest, so a skill that points at those files used to be accepted without comment and then break on the developer's machine once it was distributed. Gram now reads the manifest body for the files it references, returns them on `SkillVersion.resource_references` with their kind, and lists them in a "Supporting files" panel on the skill detail page that states the files are neither captured nor distributed and calls out when a manifest runs executable content that cannot be shown for review. Detection covers Markdown links and images, link reference definitions, and any path under a specification-reserved directory appearing in prose or code. This reports the gap rather than closing it: referenced files are still not ingested, versioned, or distributed.
