---
"server": minor
"dashboard": patch
---

Teams installing Gram-published plugins now get observability automatically.
Each org's published marketplace ships a `base` plugin containing the team's
hooks with credentials embedded — no manual SessionStart configuration, no
credential paste, no risk of forgetting the setup step. Install once per
machine and tool events flow into the Gram dashboard for the org regardless
of how many feature plugins a team member also installs.
