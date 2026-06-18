---
"server": minor
---

Custom detection rules now match via a structured `match_config` (an ANDed/ORed
list of `{target, op, value, path}` conditions over message targets — content,
tool_name/server/function, tool_args JSON paths, etc.) instead of a single
regex. The legacy `regex` column is still evaluated as a fallback via
`EffectiveMatchConfig`. The rule engine, rule CRUD, the LLM rule-suggest, the
test-rule playground, and the real-time + batch scan paths all evaluate the new
condition engine; the `risk` API and SDK expose `match_config` on create/update,
suggest, and test.
