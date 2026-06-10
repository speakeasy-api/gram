---
"server": patch
"dashboard": patch
---

Direct assistant MCP authentication prompts to the assistant's owner instead
of whoever happened to trigger the assistant. Slack onboarding now records the
owner's Slack identity in the assistant's instructions, runtime guidance
delivers OAuth links to the owner (ephemeral or DM) and tells anyone else that
the owner has to complete the connection, and prompts shown when the owner is
unknown now say explicitly that authentication is for the owner — so an
unexpected auth message is no longer mistaken for a failed setup.
