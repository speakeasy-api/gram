---
"server": patch
"dashboard": patch
---

Require proof-bound acting-user enrollment for live Claude Code and Codex prompt and tool checkpoints, and enforce the current organization `ai_access` policy before work resumes. Shared organization and environment keys remain telemetry-only; without proof-bound user enrollment, governed activity fails closed. Coverage applies only to relay versions that implement the delegation contract; this change does not claim broad coverage for earlier relay or native-client versions.
