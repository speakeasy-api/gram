---
"server": patch
---

The remote MCP connect page now matches the dashboard and asks for less. A server whose single required service is not linked yet goes straight to that provider instead of showing an interstitial with one button; on a server fronting several providers, Connect is a full-page step so each provider stays its own decision. The header names the requesting client and the MCP server side by side, so a slug carrying the organization prefix is still readable. Tool access leads with "All tools" and reveals the picker only when the grant is narrowed, and request details sit behind a disclosure — except the local-address warning, which stays visible.
