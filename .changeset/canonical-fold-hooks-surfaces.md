---
"server": patch
---

Extend canonical identity folding to the hooks pages and unproxied MCP usage:
behind the same rollout flag, the user dimension, unique-user counts, and
email drill filters fold one employee's linked emails into one identity
across the hooks summary, skill breakdown, hooks breakdown, timeseries, and
unproxied MCP server user usage. Literal behavior is unchanged with the flag
off.
