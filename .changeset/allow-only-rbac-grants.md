---
"server": patch
---

Remove legacy deny-effect RBAC grants from the server. Authorization exceptions
continue to use explicit exclusion scopes, preserving existing dashboard rule
behavior while simplifying grant storage and evaluation to allow-only rows.
