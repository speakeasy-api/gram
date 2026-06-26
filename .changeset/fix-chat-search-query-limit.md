---
"dashboard": patch
---

Stop chat-conversation search from erroring on long queries. The `chat.load` API caps `query` at 200 characters, but the find-in-conversation bar sent whatever was typed, so a long query failed with a hard validation error. The bar now gates the request at 200 characters and flags the over-limit state inline: the search icon turns into a red warning icon (with a tooltip) and the match counter shows the live `length/200` count in red.
