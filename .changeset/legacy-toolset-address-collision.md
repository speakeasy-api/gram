---
"server": patch
---

Saving a legacy toolset no longer fails with a conflict when its hosted address or server slug is already held by another server. The toolset saves and stays in its legacy state; requesting a network access mode in that state still returns a conflict.
