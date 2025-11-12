---
"server": patch
---

Updated the database query to list reapable fly apps so that it can be scoped to a specific project ID. This allows project-scoped reaping. Previously, the project-scoped reaper was not passing the project ID to the query and it was acting as a global reaper.
