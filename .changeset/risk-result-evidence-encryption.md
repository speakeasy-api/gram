---
"server": patch
---

Encrypts the customer text that chat and hook risk findings matched (`risk_results.match` and `risk_results.spans`) under each organization's own key. An organization is cut over with `gram encrypt-risk-results --organization-id <ORG_ID> --dry-run=false`. The command provisions the organization's `risk-evidence` data encryption key, after which every server and worker seals new matches with it, and then encrypts the rows already stored. Organizations that have not been cut over keep storing plaintext. Exact and regex exclusions are still applied retroactively to encrypted findings.
