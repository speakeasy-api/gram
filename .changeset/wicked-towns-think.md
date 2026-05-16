---
"server": patch
---

Fixed a bug where snapshot and metadata fields in audit log outbox entries were being base64-encoded instead of preserved as inline JSON objects.
