---
"server": minor
---

Outbound Slack messages can now render rich Block Kit content. `chat.postMessage` and `chat.postEphemeral` accept an optional typed `Blocks` field (section, actions+button, context, divider) alongside the existing text fallback. Button clicks come back as `block_actions` interactions on the existing Slack trigger webhook, are correlated to the originating thread, and reach the assistant as a new turn carrying `action_id`, `action_value`, and `block_id` — so assistants can present options and receive the user's choice in the same conversation.
