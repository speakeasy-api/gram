---
"admin": patch
---

Work the admin organization record's Projects and Members as proper views. Projects gains an MCP Servers count and drops the raw id column; Members gains a monogram, reads "Never" for a member who has never signed in, and drops the id column. Both draw oldest first, count their rows above the table, and keep their empty state. The Projects nav item keeps its count for every organization except the one-project case it already shortcuts.
