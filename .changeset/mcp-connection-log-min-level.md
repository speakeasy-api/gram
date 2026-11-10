---
"server": minor
---

MCP connection log exports now have a minimum level. The default stays at errors only. Lower it to warnings to also send blocked connections, or to everything to also send declined ones. Set it on the export, through the API, or with the Platform MCP `create_data_export` tool. Speakeasy now publishes every level and each export drops what is below its own level.
