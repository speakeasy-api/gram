---
"server": minor
"dashboard": patch
---

Directory role mapping saves now replace a source's complete role set atomically, preserve unchanged mappings, and audit added and removed roles separately. An empty set removes all mappings for that source. Older dashboard clients retain a compatibility endpoint that cannot overwrite an existing multi-role set.
