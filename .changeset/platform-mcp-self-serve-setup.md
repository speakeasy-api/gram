---
"server": minor
---

Platform MCP no longer treats putting an MCP server into a plugin as a required setup step. Once a server is ready, the person who added it can use it straight away from the same connection, or get install steps for another app. Sharing it with other people through a plugin is offered as an optional last step. `get_my_install_instructions` now also accepts the `registration_id` returned when the server was added.
