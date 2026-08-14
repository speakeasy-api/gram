---
"admin": patch
---

A pasted organization id or WorkOS id in the admin organizations search now finds a disabled organization, because investigating a suspended organization is a leading reason to paste an id. Both id matches also ignore casing, so an id that arrived lowercased from a log pipeline still lands on the right organization. The search term is trimmed of surrounding whitespace, and % and _ inside it match literally instead of acting as wildcards. An id match still respects the account type, trial state and cursor filters.
