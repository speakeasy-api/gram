---
"dashboard": patch
"server": patch
---

Editing an environment now requires `environment:write` instead of `project:write`. Creating, updating, and deleting environments previously gated on `project:write`, so principals holding only `environment:write` were rejected. The dashboard gates for these actions were realigned to match.
