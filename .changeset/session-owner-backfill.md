---
"server": patch
---

An agent session no longer shows as anonymous when its first prompt arrives before the session's identity telemetry. Once telemetry names the person, Speakeasy fills in the session's owner if it had none. It never replaces an owner already recorded and never adds one to a session an agent key produced.
