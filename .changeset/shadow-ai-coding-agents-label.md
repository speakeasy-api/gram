---
"dashboard": patch
"server": patch
---

Shadow AI now calls AI coding tools such as Claude Code, Cursor and Codex "Coding agents" instead of "Harnesses": the Shadow AI tab, its page and empty states, the scan target editor, the Shadow AI policy and the Watchdog tile all use the new name. The tab now lives at `shadow-ai/agents`; links to the old `shadow-ai/harnesses` pages redirect there. The Platform MCP's Shadow AI tools describe the category the same way. The category value stays `harness`, so filters, the API and deployed device agents are unchanged.
