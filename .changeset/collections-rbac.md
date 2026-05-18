---
"server": minor
"dashboard": minor
---

Enforce RBAC on the collections API. `List` and `ListServers` now require `org:read`; `Create`, `Update`, `Delete`, `AttachServer`, and `DetachServer` require `org:admin`. The dashboard's sidebar, collections list, and detail pages open up to `org:read` members, while create/edit/delete and server attach/detach controls stay behind `org:admin`.
