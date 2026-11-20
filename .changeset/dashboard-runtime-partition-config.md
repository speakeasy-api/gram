---
"dashboard": minor
---

Read deployment-specific dashboard settings (PostHog, Datadog RUM, tunnel host, custom domain target, Pylon app and first-party hosts) from runtime configuration supplied by the deployment instead of matching the browser host against a list of production hosts, so one dashboard image serves every partition. Removes the unused Elements Datadog RUM error tracking.
