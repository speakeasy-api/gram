---
"server": minor
---

Capture the supporting files of skills from developer devices. When the hooks relay finds the skill directory whose SKILL.md matches an activation, it reports the directory's tree digest and uploads scripts, references and assets with the new `hook.skill-content.v2` upload, so a change to a script creates a new captured skill version. Relays fall back to the SKILL.md-only upload when a server does not accept v2.
