---
"server": patch
---

mig: drop the unused `session_capture_exclusions` table. Its feature (ingest-time session-capture exclusion checks) was closed before merging, so only the orphaned migration remains to clean up. Excluding specific users from observability is instead handled by giving them a separate project, now that the observability plugin is project aware.
