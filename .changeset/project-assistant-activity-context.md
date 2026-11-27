---
"server": minor
"dashboard": minor
---

The Project Assistant now knows what the user was just doing in the dashboard. The dashboard keeps a short log of the pages the user opened, the result of each action they took, the errors it showed them, and repeated failures of the same action. When the user opens the docked assistant, the new `assistants.inferIntent` endpoint reads that log with a small model and returns the user's likely goal and blocker, a one-line opener, and up to three prompts written for that task. The dock shows them above its usual suggestions. Each message to the assistant also carries the log and the inferred goal in its `<dashboard_context>` block, so a question like "how can I see if this worked?" is answered about the action the user just took instead of with a clarifying question. The log stays in the browser and goes only to Speakeasy's server, it names API calls but never their arguments, and the endpoint stores nothing. The dashboard side is behind the `assistant-activity-context` feature flag. Chat titles and summaries now skip a leading `<dashboard_context>` block.
