---
"server": patch
---

Exclude per-request plugin download API key creation from the audit log to prevent flooding with `api_key:create` events.
