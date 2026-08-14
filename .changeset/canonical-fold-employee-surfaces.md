---
"server": patch
---

Extend canonical identity folding to the employee detail pages, the
enrollment list, and the billing email breakdown: behind the same rollout
flag, per-user metrics scope through the identity map (email identifiers
resolve entirely in-query), the enrollment list shows one row per employee,
and billing email slices fold to canonical identities. Literal behavior is
unchanged with the flag off.
