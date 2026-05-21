---
"server": patch
---

Slack-triggered assistant chats now open a fresh assistant thread for each top-level message instead of folding distinct conversations onto a single per-channel thread. Top-level Slack messages and DMs used to share one Gram thread (and one Fly runtime) per channel, so unrelated users' messages bled into the same context window.
