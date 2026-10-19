---
"server": minor
"dashboard": minor
---

Send tool approval requests to approvers as Slack DMs. Each approver on the policy, with roles expanded to people, gets a message from the Speakeasy app with the tool, the agent's reason, the redacted arguments and Approve once and Deny buttons. A click goes through the same approver check as the dashboard, and every DM shows the outcome once the request is decided or expires. New Slack installs ask for `chat:write`; workspaces installed before it report it under `missing_scopes`, and the Slack workspaces page asks admins to reinstall. Approvers can always decide in Speakeasy. `mise run zero:slack` connects a Slack test app to the local organization. Behind the `risk-tool-approvals` flag.
