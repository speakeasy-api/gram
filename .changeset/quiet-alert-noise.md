---
"server": patch
"dashboard": patch
---

AI integration syncs now stop retrying when the provider rejects the request outright (HTTP 400/401/403/404/422), show the provider's reason on the integration, and pause the schedule after repeated rejections instead of retrying forever. Chat analysis and skill efficacy judging retry transient model failures without failing the background task, prompt injection scans no longer fail an entire batch when one message is too large to publish, and expected API responses such as permission denied or not found are no longer reported as frontend errors.
