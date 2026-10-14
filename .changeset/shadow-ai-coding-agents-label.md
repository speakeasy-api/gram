---
"dashboard": patch
"server": patch
---

Shadow AI now calls AI coding tools such as Claude Code, Cursor and Codex "Coding agents" instead of "Harnesses": the scan target library, the Shadow AI policy editor, the Watchdog tile and the identity view all use the new name. A coding agent's detail page now lives at `shadow-ai/agents/<tool>`; links to the old `shadow-ai/harnesses/<tool>` pages redirect there. The Platform MCP's Shadow AI tools describe the category the same way. The category value stays `harness`, so filters, the API and deployed device agents are unchanged.
