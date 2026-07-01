---
"server": minor
---

Plugin marketplaces now send a human-readable `displayName` to Claude Code, so plugins show with their admin-entered name and capitalization (e.g. "MoonPay MCP Servers") instead of the de-slugified lowercase name ("Moonpay mcp servers"). The synthesized observability plugin displays as "<Org> Observability". The plugin `name` remains the kebab-case slug used for namespacing and claude.ai marketplace sync. Older Claude Code clients ignore the field and fall back to prior behavior.
