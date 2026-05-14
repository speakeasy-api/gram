---
"server": patch
---

Assistant platform toolsets are now served from `/platform/mcp/{slug}` instead of `/x/platform-mcp/{slug}`, lining up with the dedicated `/platform` ingress prefix so requests reach the server pod instead of falling through to the dashboard.
