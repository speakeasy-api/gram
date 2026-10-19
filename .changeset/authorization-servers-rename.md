---
"dashboard": patch
"server": patch
---

Remote Identity Providers are now called **Authorization Servers**, matching what they are: the upstream OAuth servers where each user signs in so an MCP server can act for them. The organization page, an MCP server's Authentication settings (formerly **Connected services**), the attach, modify, and remove sheets, and the Service Account picker all use the new name, and the field's hint now explains what an authorization server does instead of calling it rarely needed. Platform MCP's setup guidance uses the same name, and an authorization server it attaches is named "Authorization server".
