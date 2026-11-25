---
"server": patch
---

Fix the Platform MCP consent page needing two clicks. Its script was inline, which the production content security policy blocks, so "Give access" gave no feedback and a second click reused the already-spent link and showed "This connection link expired or was already used." The script is now served as a separate file, so the button disables and shows "Connecting…" after the first click.
