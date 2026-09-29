---
"dashboard": patch
---

Show a role's blocked permissions as blocks in the role editor. A role holding `mcp:blocked_read` without `mcp:read` used to render as `mcp:read` with no control on the row, so the block could not be told apart from the permission or changed. The row now names the exclusion scope, carries a Blocked badge, and states what it excludes with a menu to widen, narrow, or remove it. Picking the permission in the Add permissions menu now grants it beside the block instead of silently deleting the block, and re-pointing an exception at a different resource marks the form dirty so it can be saved.
