---
"dashboard": patch
---

Opening an MCP install page (`/mcp/<slug>/install`) while already signed in now loads the page instead of landing on the organization's home page. The login return target for that page is server-rendered, and the dashboard was routing to it client-side.
