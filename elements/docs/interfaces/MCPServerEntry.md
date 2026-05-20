[**@gram-ai/elements v1.33.0**](../README.md)

***

[@gram-ai/elements](../globals.md) / MCPServerEntry

# Interface: MCPServerEntry

Configuration for a single MCP server. Used by the [ElementsConfig.mcps](ElementsConfig.md#mcps)
array form when connecting to more than one server.

## Properties

### url

> **url**: `string`

The MCP server URL.

***

### name?

> `optional` **name**: `string`

Namespace prefix prepended to tools from this server with `__` as the
separator (e.g. `name__tool`) so that tools with identical names from
different servers do not collide. When omitted, a prefix is derived from
the URL.

***

### environment?

> `optional` **environment**: `string`

Environment slug to bind this server's tools to. Sent as the
`Gram-Environment` header on requests to this MCP server only.
