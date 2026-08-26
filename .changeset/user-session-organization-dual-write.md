---
"server": patch
---

Every new user session issuer, OAuth client, CIMD client grant, session, and consent row now records the organization that owns it alongside its project, so organization tenancy is present from the moment a row is written. Nothing reads the column yet and no existing behavior changes; rows created before this point keep a NULL organization until a separate backfill runs.
