---
"server": minor
---

The dashboard Project Assistant now reads its conversation straight from the chat service instead of a separate mirror. `assistants.sendMessage` takes an optional `chat_id` to continue a conversation (from `chat.list`), or omits it to start a new one — the server mints and returns the chat id. The redundant `assistants.listMessages` endpoint is removed; clients poll `chat.load` for the assistant's replies, which now surface as plain assistant messages.
