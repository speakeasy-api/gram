---
"server": patch
"dashboard": patch
---

Unproxied MCP server creation now validates the vendor URL (scheme, host, and SSRF blocklist) before storing it, matching the same guard the Inspect tab's live tool discovery relies on. The Inspect tab also skips discovery for disabled servers, the Overview chart zero-fills days with no activity and scrolls horizontally instead of overflowing, and the sidebar now labels unproxied servers and disables the "Test in Playground" link for them since the Playground can't reach a server Gram never proxies.
