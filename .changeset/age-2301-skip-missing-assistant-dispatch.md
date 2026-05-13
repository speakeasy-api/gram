---
"server": patch
---

Drop trigger dispatches whose target assistant has been deleted instead of failing the activity; retrying can't recover a missing row.
