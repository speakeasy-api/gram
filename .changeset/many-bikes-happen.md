---
"cli": patch
---

When adding duplicate sources via `gram stage`, the last occurrence of
each source slug is now retained, ensuring predictable behavior without
erroring out.
