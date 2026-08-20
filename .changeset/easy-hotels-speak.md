---
"server": minor
"dashboard": patch
---

access.createRole no longer requires a description. The Create Role dialog accepts an empty description field and omits it from the request, so roles can be created without one.
