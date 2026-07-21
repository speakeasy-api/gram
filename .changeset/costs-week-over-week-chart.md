---
"dashboard": minor
---

Add a stacked cost-over-time chart to the costs explorer so spend can be compared week over week (or day/month), stacked by the current breakdown axis, with drag-to-zoom drill-down. The chart is the billing page's token-usage panel generalized into a shared `StackedTimeSeriesPanel` component; the billing page now renders through the same component with no visual change.
