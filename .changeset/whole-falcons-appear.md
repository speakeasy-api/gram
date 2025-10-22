---
"server": patch
---

Prevent nil pointer dereference panic during server and worker shutdown. This
was happening because the Gram Functions orchestrator was retuning nil shutdown
functions at various code paths.
