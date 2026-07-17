---
"server": minor
---

Warn organization billing contacts before their managed OpenRouter credits run out. The periodic credit-usage poll now emails the billing alert contact when usage of either platform-managed key — the chat key (playground, elements, assistants, completions proxy) or the internal key (risk-policy judges, titles, resolutions, memory) — crosses 50%, 75%, 90%, and 100% of its monthly cap. Each key type has its own email template and thresholds dedup independently per key with monthly re-arming. Chat-key warnings are suppressed for organizations with a chat-serving BYOK key; internal-key warnings always apply since that usage is platform-billed. Organizations without a billing alert email are skipped.
