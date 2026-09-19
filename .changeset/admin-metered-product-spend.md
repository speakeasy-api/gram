---
"server": minor
"admin": minor
---

Add an admin spend breakdown API and organization billing visualization for every account type, including organizations without a Stripe subscription. Reuse exact server-calculated storage, per-scanner risk, and MCP egress estimates at current PAYG list prices, with product selection, billing-cycle and custom date ranges, daily/weekly/monthly grouping, and cumulative views. These usage comparisons are not invoices or contracted charges.

Display USD amounts rounded to two decimals while retaining exact arithmetic. Use consistent, theme-aware product colors across spend and usage graphs: blue for storage, purple for risk scanning, and amber for MCP.

Keep the customer spend endpoint restricted to PAYG organizations. Admin requests require the existing admin authentication and resolve the requested organization by ID or slug.
