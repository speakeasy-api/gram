---
"server": patch
"dashboard": patch
---

Add workload identity setup for inference. Workloads can exchange trusted issuer JWTs for project-bound inference tokens when their assigned agent and owner explicitly have inference permission, without MCP configuration or transcript access.
