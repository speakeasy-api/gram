---
"server": minor
"dashboard": patch
---

Organization admins can decide who may use each auth client: Use grants permission to use the client and Blocked takes it away, on every server that offers the client. Rules on personal clients never let anyone use another person's connection. Admins can also replace an existing API key's own policy with `keys.updatePolicy`, and a key's policy can only name auth clients and projects in its own organization. Both changes are recorded in the audit log.
