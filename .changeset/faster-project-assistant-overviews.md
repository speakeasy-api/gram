---
"server": patch
---

The Project Assistant answers overview questions, such as how a project is doing, what is failing or what was spent, in fewer steps and less time. The observability overview and tool usage summary tools now return compact summaries and leave out time series and cross breakdowns unless asked for them. The assistant calls these tools directly instead of searching for them first, a `select:` tool search returns only the named tools, and a tool result over 20 KB keeps a preview in the conversation and the full result in a file the assistant can read.
