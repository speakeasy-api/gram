---
"dashboard": minor
---

Restructure the costs explorer around a stacked cost-over-time chart and one unified control bar. The chart (the billing token-usage panel generalized into a shared `StackedTimeSeriesPanel`) stacks daily spend by the current breakdown axis, with weekly/monthly bars for week-over-week comparison and click/drag drill-down into a date range. The search box, breakdown axis track, CSV export, a new Reset button, the dataset selector, and the date-range picker now form a two-row control bar under the headline stats that pins to the top of the page when scrolled past. Re-pivoting or drilling updates the page in place instead of flashing back to skeletons. The billing page renders through the same shared chart with no visual change, and `Page.Toolbar` gains multi-row (`Row`/`Leading`) composition.
