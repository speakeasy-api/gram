---
"dashboard": patch
---

The billing page's token usage chart and breakdown table now report the billed population — LLM completions that run through Gram's server — instead of org-wide analytics aggregates dominated by unbilled agent-fleet telemetry (cache reads included), which overstated usage by orders of magnitude. The headline total plots the billed per-day series behind the usage card, so the chart and card match exactly; dimension and token-type breakdowns are scoped server-side, and the client no longer issues per-dimension analytics queries.
