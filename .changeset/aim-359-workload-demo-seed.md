---
"server": patch
---

Seed the demo organization with a second workload issuer that permits wildcard admission, a wildcard admission standing for an agent fleet, and its agent assignment. The existing `Acme CI` issuer keeps wildcard admission off on purpose: its subjects encode a branch ref, where a wildcard would admit anyone able to push a branch.
