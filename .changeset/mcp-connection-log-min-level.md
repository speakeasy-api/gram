---
"server": minor
---

MCP connection log exports now have a minimum level. The default stays at `error`, which sends failed connections. Set it to `warn` to also send blocked connections, or to `info` to also send declined ones. Set it on the export, through the API, or with the Platform MCP `create_data_export` tool.
