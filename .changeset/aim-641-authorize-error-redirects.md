---
"server": patch
---

Speakeasy MCP, Platform MCP, and Admin MCP now show pre-authentication authorization errors on an HTTP 400 page with an explicit return link for remote and custom-scheme callbacks. Automatic error redirects remain available for registered HTTP and HTTPS callbacks on `127.0.0.1`, `::1`, and `localhost`. The page names the MCP client and server, explains the error and the next step in plain language, and keeps the error code, description, and a support reference under a collapsible technical details section with a copy button. Consent-denial and post-authentication redirects are unchanged.
