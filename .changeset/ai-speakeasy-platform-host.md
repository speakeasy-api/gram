---
"dashboard": patch
"server": patch
---

Serve the full product on extra first-party hosts listed in `GRAM_PLATFORM_HOSTS` (such as `ai.speakeasy.com`) alongside the server URL's host. Login started on such a host calls back and lands on that same host, and the dashboard reports telemetry for `ai.speakeasy.com` to the production projects.
