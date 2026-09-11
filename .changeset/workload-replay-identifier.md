---
"server": patch
---

Derive a workload assertion's replay identifier when the platform sends no `jti` — Google sends none and Microsoft Entra calls it `uti`. Client assertions still require one.
