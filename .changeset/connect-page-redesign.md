---
"server": patch
---

The remote MCP connect page now matches the dashboard and asks for less. A server whose single required service is not linked yet goes straight to that provider instead of showing an interstitial with one button; on a server fronting several providers, Connect opens a popup so the page keeps its state. Tool access leads with "All tools" and reveals the picker only when the grant is narrowed, and request details sit behind a disclosure — except the local-address warning, which stays visible.
