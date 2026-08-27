---
"server": patch
---

Strip OpenClaw's inbound-metadata envelope (conversation info, reply targets, chat history, delivery hints, timestamp prefix) from turn text before generating session titles and summaries, matching the harness framing already stripped for Claude Code.
