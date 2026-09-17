---
"server": patch
---

Route PostHog SDK logs through the structured logger and evaluate feature flags individually so local evaluation warnings no longer land as errors on every flag check.
