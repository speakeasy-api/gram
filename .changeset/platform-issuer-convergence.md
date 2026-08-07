---
"server": minor
"dashboard": minor
---

Platform admins can now consolidate an organization's remote identity provider onto the shared platform catalog entry for the same upstream. A new Convergence tab on a platform provider lists the organizations running their own provider for that upstream, along with how many clients would move and any metadata differences, and consolidating one re-points those clients without anyone having to sign in again. Providers whose issuer URL differs only by a trailing slash or an explicit default port now count as the same upstream, since those near-duplicates are the ones most worth folding together.
