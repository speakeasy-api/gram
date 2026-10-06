---
"server": minor
"dashboard": minor
---

The MCP server Team Access tab gains a Check access section: pick an organization member to see whether they can connect to, view, and manage the server, and which rules decide it. The new `access.explainResourceAccess` endpoint returns the decision from the same evaluation as runtime enforcement, naming each rule's source, whether it is blocked or overridden, and the directory role mapping behind a role for organization admins.
