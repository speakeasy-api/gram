---
"server": patch
---

Platform tools such as Slack now run when an agent or organization gateway calls them through `execute_tool`. The call runs in the project of the server that hosts the tool, instead of failing with "platform tool requires project auth context".
