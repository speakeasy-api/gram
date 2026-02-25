---
"dashboard": patch
---

Prevent source detail page crash when logs are not enabled. The telemetry query now uses useLogsEnabledErrorCheck and throwOnError: false to gracefully degrade without metrics instead of crashing the entire page.
