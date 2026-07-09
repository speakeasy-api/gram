---
"dashboard": patch
---

The billing page's Model breakdown now splits into "Risk Policy Analysis Model" — the platform's own risk-policy scanning inference, the metered unit of the TUM contracts — and "Completion Model" for user-facing completion surfaces (playground, elements, MCP chat, Slack). The "Sessions & messages" section and the risk-findings chart stacking are removed: billing meters the act of scanning observed traffic, not the customer's message population. Risk-analysis inference is attributed to the scanned user, so the User, Role, and Division breakdowns now report whose traffic was analyzed.
