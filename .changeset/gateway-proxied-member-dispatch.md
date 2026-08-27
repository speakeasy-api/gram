---
"server": minor
---

Serve remote and tunneled gateway members: describe_server, describe_tools, and execute_tool now dispatch to proxied member upstreams with strict per-member credential routing, member-scoped outage degradation, and live tunneled member status in list_servers.
