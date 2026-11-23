---
"server": patch
---

A rule that blocks MCP tools by name or by kind (read-only, destructive, unannotated and so on) now also blocks a call whose tool name or kind cannot be determined. Before, such a call was outside the rule. Rules that allow tools by name or kind already refused it.
