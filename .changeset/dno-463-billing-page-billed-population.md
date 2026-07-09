---
"dashboard": patch
---

The billing page now reports the billed population exactly. The chart's headline total plots the billed per-day series behind the usage card (matching it to the token), and every breakdown — model, user, division, role, source, token type — reads the new dimensioned billing aggregate with the same qualification and registry-driven source scoping as the billed totals, instead of org-wide analytics aggregates dominated by unbilled agent-fleet telemetry (cache reads included) that overstated usage by orders of magnitude. Assistants usage is tagged but excluded from the billed scope until BYOK. Dimensions billed completions don't carry (provider, account type, skill, MCP server/tool, cache token types) leave the billing page; they live on the costs/insights pages.
