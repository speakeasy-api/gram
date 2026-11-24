---
"server": patch
---

Saving a role with a rule that cannot be stored, such as an `mcp:write` rule limited to read-only tools, now answers 400 with the reason instead of 500. Nothing changes in what is stored: the rule was already refused.
