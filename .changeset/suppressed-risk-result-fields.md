---
"server": minor
---

Risk results now carry converged suppression fields: `suppressed_at`, `suppressed_reason` (`rule` | `manual` | `automated`), `suppressed_detail`, and `exclusion_id`. The dismissed-findings listing populates them for every row; `false_positive_at` remains as a deprecated mirror of `suppressed_at` while clients migrate.
