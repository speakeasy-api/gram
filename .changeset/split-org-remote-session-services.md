---
"server": minor
"dashboard": patch
---

Split the org-admin `organizationRemoteSessionIssuers` service into three per-resource services mirroring the project-scoped layer: `organizationRemoteSessionIssuers`, `organizationRemoteSessionClients`, and `organizationRemoteSessions`. Pure refactor with no behavior or RBAC change, but breaking for the management API and SDK: every method drops its redundant resource suffix, so the RPC paths and SDK method names change (e.g. `organizationRemoteSessionIssuers.createClient` becomes `organizationRemoteSessionClients.create`).
