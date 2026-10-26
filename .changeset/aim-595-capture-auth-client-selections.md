---
"server": patch
---

New sign-ins, ID-JAG exchanges, workload grants and dashboard-minted sessions now record which typed auth client serves each MCP server they reach, alongside the upstream OAuth client they already record, so credentials issued after the upstream OAuth migration stay covered without another run. Choices several auth clients could serve are still recorded without one and reported by the migration. How requests authenticate upstream does not change.
