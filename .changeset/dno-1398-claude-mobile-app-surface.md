---
"server": patch
"dashboard": patch
---

Claude chats imported from the Anthropic Compliance API that were started in the Claude iOS or Android app are now labeled **Claude Mobile App** (`claude-chat-mobile`) instead of Claude Chat Web or Claude Chat Desktop, and the agent filter on Agent Sessions lists them as their own surface. The app is recognized by a Claude token next to a mobile OS token in the chat's user agent. A browser on a phone still counts as Claude Chat Web. Chats imported under the old labels switch to Claude Mobile App when their next message is imported, because the importer now re-classifies each chat from its stored user agent.
