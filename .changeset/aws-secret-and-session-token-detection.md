---
"server": minor
---

Secret scanning now flags AWS secret access keys and session tokens, not just the access key id — and masks them while leaving the access key id (an identifier, not a secret) visible.
