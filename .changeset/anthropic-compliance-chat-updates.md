---
"server": patch
---

Fix the Anthropic Compliance import missing messages added to a Claude chat after its first import. The importer treated `claude_chat_updated` activities as the signal that a chat had new messages, but Anthropic records that activity for metadata edits only (name, model) and the activity feed has no message-level activity, so a chat was imported once and never revisited. The `anthropic_compliance` schedule now walks the org-wide chat list ordered by `updated_at`, which Anthropic documents as the way to pick up chats with new messages, and window-polls the activity feed for `claude_chat_created` only to classify new chats as Claude web or desktop. Saving the integration now also probes the chat list, so a key without chat content access is refused at save time. Existing integrations restart from a bounded 24-hour window on their next poll; replays are idempotent.
