---
"server": patch
---

Speakeasy no longer publishes plugins for a disabled organization. The hourly plugin sweep skips its projects, and any publish already queued when the organization was disabled is refused without retries, so its plugin marketplace gets no new commits and no new plugin API keys.
