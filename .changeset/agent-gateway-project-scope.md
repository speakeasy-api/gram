---
"server": patch
---

An agent scoped to a project now reaches only that project's MCP servers through its agent gateway, and its tool calls run in that project. Platform tools such as Slack work for these agents instead of failing with "platform tool requires project auth context". Organization-wide agents keep reaching servers in every project.
