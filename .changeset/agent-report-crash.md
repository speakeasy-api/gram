---
"server": minor
"dashboard": patch
---

Device agents can report crashes, recovered panics and update rollbacks to the control plane, including agents on servers and CI that authenticate with an agent key. Only reports shaped like a device agent crash are accepted; they are scrubbed again on arrival, forwarded to monitoring as logs and a crash count, and not stored.
