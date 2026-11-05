---
"server": patch
"dashboard": patch
---

In Tool Logs, the gateway name on a call that reached an agent gateway or the organization gateway now opens that gateway's page. It used to open the stored gateway page, which has nothing for these gateways and failed to load. An agent gateway opens the agent's Permissions tab and the organization gateway opens its own page, the same places the "dispatched through gateway" icon already opens. Tool usage traces now report the kind, project and agent of the gateway a call reached.
