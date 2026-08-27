---
"server": patch
---

Treat dial failures to a session-pinned tunnel-gateway as session-gone (HTTP 404) with a short cluster-internal connect timeout, and unpublish the dying pod's routes on SIGTERM, so gateway rollouts no longer stall clients for 30s and burst 502s.
