---
"server": patch
"dashboard": patch
---

Token numbers on the costs page (KPI tile, time series, breakdown table, session drill-down, entity profile, CSV exports) and the project overview widgets now show plain LLM tokens (input + output), matching the employee pages. The TUM billing population (which additionally counts cache writes) is no longer shown on person-facing surfaces; cache-creation tokens remain visible as their own explicitly labeled metric.

The employee page's tool-call counts now include hook-reported tool calls alongside Gram MCP tool spans (the same shape as the data-flow graph), so scoping to a single account no longer shows zero tool calls.
