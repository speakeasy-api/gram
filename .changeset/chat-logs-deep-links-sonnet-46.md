---
"dashboard": patch
---

Add stable URL deep-links for agent sessions in Chat Logs — the selected
session now syncs to a `chatId` search param so a URL like
`/logs?chatId=<id>` is shareable and survives reload. Also upgrade the
default AI Insights model from claude-sonnet-4.5 to claude-sonnet-4.6.
