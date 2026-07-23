---
"server": minor
---

Secret scanning no longer flags an AWS access key id as a leaked secret — it's an identifier, used only to anchor detection of the co-located secret access key. Findings now mask just the secret value, not its surrounding label.
