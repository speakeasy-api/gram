---
"server": minor
---

The suppressed risk results listing (`risk.listDismissedResults`) now covers every suppressed finding — rule exclusions included, previously absent — and accepts an optional `reasons` filter (`rule` | `manual` | `automated`). Legacy pre-convergence rule rows derive their reason from the exclusion id instead of reading as manual.
