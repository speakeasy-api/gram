---
"server": patch
---

A permission rule whose resource is the nil UUID (`00000000-0000-0000-0000-000000000000`) is now refused when saved, for roles, direct grants and agent policies. Such a rule could never match a real resource.
