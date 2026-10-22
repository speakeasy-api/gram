---
"server": patch
"dashboard": patch
---

The stable device agent installer links move from `/v1/install/` to `/rpc/install/` (`device-agent-macos.pkg` and `device-agent-windows.msi`). Production never routed `/v1/install` to the server, so those links returned the dashboard's HTML, and the Windows setup step saved that page as `speakeasy-agent.msi`. The Windows step now uses the new link.
