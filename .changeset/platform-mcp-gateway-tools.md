---
"server": minor
---

The Platform MCP can now create gateways and set their members. `create_gateway` creates a private gateway with its default address and an ordered list of member MCP servers in one step, and `set_gateway_members` replaces a gateway's members with an exact ordered list. Both apply all their changes together or none: a server that cannot sit behind a gateway is named with the reason.
