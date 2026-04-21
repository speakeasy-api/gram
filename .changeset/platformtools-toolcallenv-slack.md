---
"dashboard": patch
---

Thread `toolconfig.ToolCallEnv` through `PlatformToolExecutor.Call` so platform tools can read per-call env (OAuth token, user/system env, Gram email). Add eight Slack Web API platform tools under `server/internal/platformtools/slack/` registered via the existing factory list; unused until the assistants runtime wiring lands.
