---
"server": patch
---

Name the owner of a refused, already-used refresh token on its connection row, and name a gateway member's upstream host on request rows where that upstream itself failed.

For deployers: refused used-token rows now carry a subject; member upstream failures (rejected credential, rate limited, server error, unreachable) now carry the upstream host. In daily outcomes, fix pairing and totals are unchanged; repeated refusals now fold per person.
