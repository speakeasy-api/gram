---
"server": patch
---

Every request authorization now refuses a disabled organization, including assistant runtime tokens and MCP serving, and triggers in a disabled organization no longer dispatch. Previously only sign-in, dashboard sessions, API keys and a few credential types checked.
