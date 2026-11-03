---
"dashboard": patch
---

Claude Cowork onboarding now has a dedicated step for allowing Speakeasy in Claude's network egress settings, placed before OTEL export, on both the Team/Enterprise and Pro/Max paths. It names the exact Claude settings (Capabilities → Code execution and file creation → Allow network egress → Additional allowed domains), lists every Speakeasy domain to copy, and notes that the hooks binary download also comes from those domains, so nothing else needs allowing. A separate final step covers verifying hook events in a new Cowork session.
