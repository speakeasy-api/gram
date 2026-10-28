---
"server": patch
---

Keep agent connections working when their assigned upstream session is reauthenticated. Assignments follow current credentials on the same session row; deleted or revoked sessions still require explicit reassignment.
