---
"dashboard": patch
---

Dashboard links and setup values now follow the host you are on, so `ai.speakeasy.com` and `dev.ai.speakeasy.com` get the right tunnel gateway, custom domain CNAME, telemetry endpoints and allowlist host. The explore-demo link stays on the current host instead of sending you to `app.getgram.ai`, which would log you out.
