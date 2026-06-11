---
"server": minor
---

Add risk exclusions: suppress false-positive risk findings by exact value, regex, rule_id, source, or presidio entity type, scoped per-policy or globally. Exclusions are applied going forward by the scanner and retroactively by a Temporal reconcile sweep that flags matching rows in `risk_results` (no presidio re-run); removing an exclusion restores the findings. Exposes `risk.exclusions.{list,create,update,delete}` on the management API.
