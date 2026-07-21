---
"dashboard": minor
---

Add a stacked cost-over-time chart to the costs explorer, stacked by the current breakdown axis with daily/weekly/monthly bars for week-over-week comparison and drag-to-zoom drill-down. The chart is the billing page's token-usage panel generalized into a shared `StackedTimeSeriesPanel` component; the billing page now renders through the same component with no visual change.
