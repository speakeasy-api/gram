---
"server": minor
"dashboard": patch
---

Organization admins can list the auth clients Speakeasy uses to reach upstream MCP servers, see which servers offer each one, rename, enable or disable a client, and stop a server offering it. Each server now states whether its upstream needs authentication, so a server that needs it but offers no enabled client reads as incomplete setup rather than as needing none. Stored header values are never returned, and every change is recorded in the audit log.
