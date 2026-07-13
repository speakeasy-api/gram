---
"server": patch
"dashboard": patch
---

feat: expose `is_default` on the plugin API and use it in the dashboard instead of matching on the "Default" name/slug. The onboarding distribute-servers step and plugin card/detail pages previously identified the org's fallback plugin by string comparison (`name === "Default"` / `slug === "default"`), a proxy that predates the server's `is_default` column and unique-per-project index. Both now read the real `is_default` flag returned by `listPlugins`/`getPlugin`.
