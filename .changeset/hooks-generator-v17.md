---
"server": patch
---

Bump hooksGeneratorVersion to 17 so the next hooks rollout republishes every connected plugin. Ships the pinned-bootstrapper generation to orgs still on older script generations, and is the vehicle for distributing the hooks@0.2.0 binary (fail-open cache + offline spool) once hooksBinaryVersion is re-pinned to the published release.
