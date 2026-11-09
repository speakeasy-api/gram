---
"server": patch
"dashboard": patch
---

Tool usage reads for an agent's identity page now find the agent's traces before grouping them, so they finish inside ClickHouse's time limit on busy projects. Filter options load only the groups the page shows. A totals read that stops partway returns an error instead of zero calls. Tool logs say when totals or the timeline could not load, with a Retry, and the dashboard no longer retries a read the server answered with an error.
