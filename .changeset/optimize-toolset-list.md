---
"server": patch
---

Avoid rebuilding every platform tool descriptor for each tool returned by `toolsets.list`, significantly reducing latency for projects with large toolsets.
