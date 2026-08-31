---
"server": minor
---

MCP endpoints now validate the `Origin` header and answer cross-origin browser requests with 403, as the MCP specification requires, and reject POSTs sent with a form or plain-text `Content-Type` with 415. Native MCP clients are unaffected because they send neither `Sec-Fetch-Site` nor `Origin`, and Gram Elements keeps working through the chat-session token's audience claim.
