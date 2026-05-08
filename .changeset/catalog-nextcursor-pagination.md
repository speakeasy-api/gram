---
"server": patch
---

Fix catalog registry pagination so infinite scroll fetches all entries beyond the first page.

`ListServers` now returns the upstream registry's `nextCursor` alongside the server list. `ListCatalog` passes that cursor through to the API response so the frontend's `getNextPageParam` receives a non-null value and `hasNextPage` becomes `true`. Previously `NextCursor` was always `nil`, causing the intersection observer to never trigger a second fetch and silently dropping any entries past the first 50.
