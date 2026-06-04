---
"server": minor
---

Project Assistant turns sent from the dashboard now run under the sender's user identity instead of the user who first enabled the assistant for the project. MCP tool calls, audit attribution, and any per-user RBAC inside a turn reflect the user who actually sent the message. Non-interactive sources (cron, wake), Slack-sourced turns, and system-initiated MCP-auth resumptions continue to run under the assistant's creator.
