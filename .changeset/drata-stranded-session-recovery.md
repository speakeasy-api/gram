---
"server": patch
---

fix: cancel stranded Drata sessions before pushing coverage evidence

Drata permits only one IN_PROGRESS upload session per custom-connection
resource, so a push that died mid-upload left a session that blocked every
later push. Each push now sweeps and cancels any stranded session before
opening its own.
