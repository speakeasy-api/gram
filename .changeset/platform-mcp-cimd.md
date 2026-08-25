---
"server": patch
---

The Platform MCP authorization server now accepts OAuth Client ID Metadata Documents (CIMD) alongside dynamic client registration. A client that presents a URL-shaped `client_id` has its metadata document fetched, validated, and cached against the stored client row, so agents that prefer CIMD over dynamic registration can connect instead of failing at `/authorize` with no fallback. Loopback redirect URIs match ignoring the port for such clients, as RFC 8252 requires for native apps that bind an ephemeral port per run, and the consent screen shows the document's origin next to the client name.
