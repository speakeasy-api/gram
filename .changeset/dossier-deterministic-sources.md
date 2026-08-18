---
"server": minor
"dashboard": minor
---

The MCP evidence dossier gains three deterministic sources. The assembler now
consults the code host about a package's declared source repository (stars,
forks, contributors, commit recency, archived status), asks OSV.dev which
published vulnerability advisories name the package, and reads the domain
registry's registration record for a remote server's registrable domain.
Package registries also surface their declared repository and homepage URLs.
Each source follows the dossier's existing contract — found, not-found, and
could-not-look stay distinct, with failures recorded as gaps — and the
approval page renders the new facts in the evidence panel, including a
dedicated advisories group where checked-and-clean is shown as a finding
rather than an absence.
