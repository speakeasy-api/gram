---
"server": patch
---

Platform MCP install instructions, member connection status, the plugin device agent link, and the organization gateway URL now use the organization's stored default platform host, which is the host the dashboard shows. Without a stored host, they follow the platform host the request arrived on, so the device agent link now also follows that host instead of always using the configured dashboard URL. Requests through a custom domain or private network keep the configured URLs.
