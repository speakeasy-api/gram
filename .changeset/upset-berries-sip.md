---
"@gram/server": patch
---

A database migration to support Gram Functions is added which includes:

- A new table called `fly_apps` to store details about provisioned fly.io apps.
- Columns in both `projects` and `deployments_functions` tables that allow pinning to a specific version of the Gram Functions runner.
