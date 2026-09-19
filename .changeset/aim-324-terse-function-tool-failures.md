---
"@gram-ai/functions": patch
"server": patch
---

Report a failed Gram Function tool call tersely. A JavaScript stack trace through the deployed bundle is now stripped from the tool output an MCP client receives, and a failure caused by input validation names each offending input on one line instead of repeating Zod's issue list as a pretty-printed JSON dump. The untrimmed body is still recorded in tool call logs, where a function's author debugs from.
