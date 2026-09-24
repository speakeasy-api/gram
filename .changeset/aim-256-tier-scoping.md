---
"server": patch
---

Tighten tier scoping on the workload identity trust policy. An admission now resolves its issuer at the tier it is being written to, so an organization-tier admission can no longer bind a project-tier issuer; where the same issuer URL is registered at both tiers the project row wins, matching how the verification path resolves it, instead of being refused as ambiguous. Reading and withdrawing a single admission are scoped by project the way the list already was. The admitted-subject list applies the issuer's visibility predicate to its join, so an admission cannot surface an issuer the caller cannot otherwise see. Withdrawing one tier now keeps the shared agent assignment while the other tier's admission is still live, instead of leaving it with no policy.
