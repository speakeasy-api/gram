---
"server": patch
---

Remote session refresh now recognizes upstream token endpoints that report a dead refresh grant outside the RFC 6749 §5.2 `error` member (an `errors` array carrying `invalid_grant - ...`, or a known vendor `error` object), so the dead grant is cleared once instead of being retried on every request.
