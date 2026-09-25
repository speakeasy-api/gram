---
"dashboard": patch
---

Rank command palette (⌘K) results by intent. On every keystroke the palette sends the typed text and a short list of prefiltered candidates to `launcher.judge`, re-orders the list from Jev's probability distributions, and shows a green ↵ on the top row when the intent is settled. Jev can also pick a verb per row: open, enable or disable an MCP server, or publish the plugin marketplace; mutating verbs always require a second Enter inside the palette. Without a resolvable OpenRouter key the palette behaves as before.
