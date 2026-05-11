---
"dashboard": patch
---

Assistant onboarding now installs a Slack app reliably end-to-end: the install card stays in view until you click "I've installed it", a single approval grants both the bot and user OAuth tokens, and the generated manifest can no longer be rejected by Slack. Slack-touching assistants now get a Slack trigger by default — additive with any cron or other trigger you asked for — so the bot is reachable bidirectionally and you can talk back to it.
