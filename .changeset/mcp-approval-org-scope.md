---
"server": minor
---

MCP approval requests are organization-scoped: one review per server per organization, whatever project the ask was raised from. The dedupe key, every query bound, and the API's tenancy all move from project to organization (project survives as provenance of where the ask was raised), the management endpoints drop the project header, and the exposure evidence widens to the organization's traffic across all its projects.
