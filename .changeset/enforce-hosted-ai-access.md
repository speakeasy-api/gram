---
"server": minor
"dashboard": minor
---

Gram-hosted inference now enforces active `ai_access` prescriptions for validated current-user sessions before provider egress, while explicitly excluding unattributed, assistant, internal, and background model calls. Matched denials preserve the administrator-selected safe note in supported chat and dashboard surfaces.
