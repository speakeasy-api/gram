---
"server": patch
---

Blocking an AI tool now reaches tools that publish no verifiable identity. Most AI tools do not publish a client ID metadata document, and blocking one of those previously recorded a decision that nothing acted on. A second check now runs on every MCP request, after authentication, against the name the client reports about itself at initialize, and refuses a tool the organization has blocked.

Both checks enforce the same decision, so "blocked" continues to mean one thing. The pre-authentication check on a verified client id runs first and is unchanged; this one runs afterwards on what is left, so a tool must pass both. A self-reported name is only ever grounds to refuse: it never admits a caller and never relaxes a decision reached from a verified credential.

Tools that were only ever known by the name they report are therefore enforceable now, and the dashboard shows a decision about them as blocked rather than unreviewed.
