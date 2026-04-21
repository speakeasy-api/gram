---
"dashboard": patch
---

Trigger infrastructure additions: `App.RegisterDispatcher` for post-construction dispatcher wiring; short-circuit Slack `url_verification` in `AuthenticateWebhook`; drop the `thread_ts`→`ts` fallback so top-level DM/channel messages correlate on the channel alone; populate `Task.EventJSON` and surface `bot_id`/`app_id` on Slack trigger events.
