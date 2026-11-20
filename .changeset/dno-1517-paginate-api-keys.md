---
"server": minor
"dashboard": patch
---

`keys.list` can now return one page at a time. Send `limit` (1 to 100) to get a page, then pass the returned `next_cursor` as `cursor` to get the next one. Keys come newest first. Send `search` to only return keys whose name contains that text, ignoring case. Callers that send no `limit` still get every key in one response, as before. The dashboard's API Keys page now loads 50 keys at a time with a "Load more" button and searches on the server, so it stays fast for organizations with thousands of keys.
