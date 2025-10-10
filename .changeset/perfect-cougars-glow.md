---
"@gram/server": patch
---

Properly set schema $defs when extracting tool schemas. Resolves an issue where recursive schemas were being created invalid.
