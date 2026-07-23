---
"server": minor
---

Secret scanning no longer flags an AWS access key id as a leaked secret — it is an identifier (AWS logs it in CloudTrail), so it is used only as an anchor for detecting the co-located secret access key, never surfaced on its own. Findings now report the secret value rather than the surrounding label, so masking covers the secret and leaves the field name visible, and a specific rule deterministically wins over the generic catch-all when both match the same value.
