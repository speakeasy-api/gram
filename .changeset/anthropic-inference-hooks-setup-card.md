---
"dashboard": minor
"server": patch
---

Add a "Set up Anthropic observability" card to organization setup that turns on Anthropic inference hooks, which capture ordinary Claude conversations rather than only the ones a coding agent has. Speakeasy mints the endpoint Claude posts each conversation to; the card walks an admin through Claude.ai's own inference hook settings in one pass — pasting the endpoint, switching Enforce verdicts on, starting the rollout in Shadow mode — and saves the signing secret Claude reveals. It confirms traffic from the conversations the hook delivers rather than from hook events, which an inference hook never produces. The webhook URL and signing secret controls are now shared with the integrations sheet instead of written twice. The marketplace, Cowork and Claude Code card keeps every section it had under the name "Set up Anthropic admin controls", and is hidden by default.
