---
"server": patch
---

Slack platform tools on a server with no Slack token, a token Slack rejects, or a token without a needed scope now fail with a tool error that says what is missing and who can fix it, instead of a generic internal error.
