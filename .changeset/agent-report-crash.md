---
"server": minor
"dashboard": patch
---

Device agents can report crashes, recovered panics and update rollbacks to the control plane. Reports are scrubbed again on arrival, forwarded to monitoring as logs and a crash count, and not stored.
