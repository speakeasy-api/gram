---
"server": patch
---

Connection outcome rows written later from held-back repeats keep their request's trace id. A failed provider refresh no longer waits up to 5 seconds on a binding lookup, and skips it for organizations with logs off. Shutdown now joins the connection outcome writer before ClickHouse closes and counts rows it could not write as dropped.
