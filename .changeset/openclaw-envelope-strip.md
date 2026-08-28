---
"server": patch
"dashboard": patch
---

Hide OpenClaw's inbound-metadata envelope (conversation info, reply targets, chat history, delivery hints, timestamp prefix) when rendering session transcripts and when generating session titles and summaries, matching the treatment of the assistant runtime's `<message-context>` framing. Stored messages are unchanged.
