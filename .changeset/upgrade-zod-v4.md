---
"@gram-ai/functions": minor
"@gram-ai/create-function": minor
---

**BREAKING**: Dropped zod v3 support. All schemas now require zod v4.

The MCP template (`gram-template-mcp`) now scaffolds projects with `zod@^4`.
Users on zod v3 must upgrade to v4 before updating to this version.
