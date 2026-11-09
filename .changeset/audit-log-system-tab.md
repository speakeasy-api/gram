---
"server": minor
"dashboard": minor
---

The audit log page now opens on a **Team changes** view that hides Speakeasy's own background activity, such as publishing plugins and creating the API keys they use. That activity moved to a new **Automated changes** view, **All changes** shows everything, and the security view is now called **Access**. Older `?category=configuration` links open the Team changes view. The `auditlogs.list` and `auditlogs.listFacets` endpoints take a new optional `origin` filter (`user` or `system`). You can also filter the list by date range and by resource: pick one from the Resource filter or use the filter button that appears beside a resource when you hover a row.
