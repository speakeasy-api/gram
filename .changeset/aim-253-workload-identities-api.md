---
"server": minor
---

Add the workloadIdentities management API: read an organization's workload identity trust policy, register and withdraw the external issuers it trusts, and admit and withdraw the subjects those issuers may present. Admitting a subject assigns the agent it inherits its policy from in the same transaction, and withdrawing an issuer withdraws its admitted subjects, so neither half-configured state is reachable. Every write returns the whole policy. Gated on `workload:read` and `workload:write`, and audited under the `workload-issuer` and `workload-admission` subjects with before and after snapshots.
