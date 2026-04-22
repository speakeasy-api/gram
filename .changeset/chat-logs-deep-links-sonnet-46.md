---
"dashboard": patch
---

- Add stable URL deep-links for agent sessions in Chat Logs — the selected
  session now syncs to a `chatId` search param so `/logs?chatId=<id>` is
  shareable and survives reload.
- Upgrade the default AI Insights model from claude-sonnet-4.5 to
  claude-sonnet-4.6.
- Insights sidebar now opts into tool-output byte capping (50KB per MCP tool
  call) and tighter auto-compaction (60% of the model's context ceiling) to
  avoid "prompt is too long" errors on long tool-heavy conversations.
