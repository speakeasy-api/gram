---
"dashboard": patch
---

The billing page's usage card now follows the selected time range instead of always showing the full billing cycle. A custom range (typed, calendar-picked, or bar-click drill-down) shows the range's billed tokens, a time-attributed overage figure (tokens recorded after the cycle's cumulative usage crossed the allowance, crossing day prorated), and the per-unit average over the range window; the allowance figure, percentage, and meter remain cycle-only concepts and hide on partial ranges. The details table's Overage column uses the same attribution for ranges instead of showing an em dash, so the card and table always agree.
