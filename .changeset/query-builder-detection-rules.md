---
"dashboard": minor
---

Author custom detection rules with a structured condition builder instead of a
single regex field: add/remove condition rows, each a target (`tool_server`,
`tool_args` JSON path, content, …) + operator (`equals`, `contains`,
`starts_with`, `regex`, `exists`, `is any of` for a value set, …) + value,
combined with AND/OR. `is any of` takes a comma-separated list (e.g. match tool
calls across servers X and Y in one row). Regex conditions accept RE2 inline
flags such as `(?i)`. Maps to the rule `match_config` API.
