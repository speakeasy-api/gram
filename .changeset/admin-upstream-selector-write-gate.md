---
"server": patch
---

The admin server now reads the upstream client selector write flag from PostHog, the same way the API server and worker do. Before this, admin actions that write upstream client selectors were always refused.
