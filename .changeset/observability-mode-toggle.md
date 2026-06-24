---
"server": minor
"dashboard": minor
---

Add an organization-level observability mode that makes generated hook plugins fully non-blocking. When enabled, hooks only observe and report and can never deny or delay a tool call. Defaults off, preserving existing behavior. Toggle it from the organization logging settings.
