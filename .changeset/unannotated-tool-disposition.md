---
"server": patch
"dashboard": patch
---

A tool with no annotations is now classified as unannotated. A rule with no annotation limit still covers it, a rule limited to read-only tools no longer does, and a tool that says it is both read-only and destructive is treated as destructive. The access, agents and Platform MCP APIs accept `unannotated` as a rule's annotation, and the role editor classifies tools the same way the server does and keeps a rule's unannotated selectors when another annotation is edited.
