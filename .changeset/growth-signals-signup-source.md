---
"server": patch
---

Report first-time signups as `gram_activity`, distinguishing an invited arrival from an organic one. The classification is made at user creation, the only moment it is knowable: a live invitation addressed to the new user means somebody asked them to join, and a moment later that invitation is accepted and the evidence is gone.
