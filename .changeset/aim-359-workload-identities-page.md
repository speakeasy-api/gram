---
"dashboard": minor
---

Add the Workload Identities page: trust an external issuer, admit the subjects it asserts, and assign the agent each admitted workload inherits its policy from. Admitting a subject and assigning its agent happen in one action, and withdrawing an issuer withdraws the subjects admitted under it. Wildcard admission is offered only where the issuer permits it, and a rule that would match nothing — a `*` in an exact subject, a missing or misplaced terminator — is flagged next to the field rather than refused on submit. `workload:read` to view, `workload:write` to change.
