---
"server": minor
"dashboard": minor
---

The Access Hub is now an organization-wide page at `/<org>/access-hub`, listed in the organization sidebar under Secure for anyone holding `workload:read` or `workload:write`. Old project URLs, including a trusted platform's page, redirect there. The `workloadIdentities` API no longer needs a project when called from a dashboard session, which reads and writes the organization tier; API-key callers still name a project, and `project_scoped` requires one.
