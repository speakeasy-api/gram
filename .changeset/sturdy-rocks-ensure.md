---
"server": patch
---

Fixes a race condition where concurrent `collections.List` calls could fail with `"default registry collection already exists"` while bootstrapping the default Registry collection. The ensure routine now treats unique-constraint violations as success and re-fetches the existing rows.
