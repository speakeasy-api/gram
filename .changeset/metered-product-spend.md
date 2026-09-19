---
"server": minor
"dashboard": minor
---

Add a metered-product spend breakdown endpoint and stacked billing chart for agent session storage, per-scanner risk scanning, and MCP egress. Show exact server-calculated estimates at current PAYG list prices with daily, weekly, monthly, cumulative, product, and date-range controls. Inference, credits, discounts, taxes, and billing adjustments are excluded; these ordinary-usage estimates are not invoices.

Restrict spend estimates to PAYG organizations using server-owned availability. Other plans return `unsupported_plan`, empty products, and a `"0"` total without querying usage; the dashboard hides the entire spend section while retaining the ordinary usage explorer.
