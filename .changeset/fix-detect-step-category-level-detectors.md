---
"dashboard": patch
---

Fix the risk policy creation Detect step so the Continue button enables when only category-level detectors (Prompt Injection, Shadow MCP, Destructive Tools, Destructive CLI Commands) are selected. These categories have no individual sub-rules, so the previous `hasEnabledDetector` check treated them as empty and kept Continue/Save disabled.
