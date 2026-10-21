---
"server": patch
---

Add a `-rekey-removal-records` mode to the hosted MCP wrappers migration tool. It moves soft-deleted toolset-keyed plugin server rows onto the canonical server key, so plugin removals keep blocking role delivery once the toolset key is dropped.
