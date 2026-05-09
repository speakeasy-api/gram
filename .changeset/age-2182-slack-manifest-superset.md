---
"dashboard": patch
---

Always grant the full Slack bot-scope superset in the assistant onboarding manifest builder, regardless of which platform tools are attached. Slack manifests are static post-install — adding a scope later forces the user to delete the app and re-OAuth — so per-tool scope gating only locked future capabilities behind a forced re-install.
