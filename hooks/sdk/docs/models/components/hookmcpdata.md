# HookMCPData

MCP feature payload.


## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `Command`                                                | `*string`                                                | :heavy_minus_sign:                                       | MCP server command, when available.                      |
| `ResultJSON`                                             | `*string`                                                | :heavy_minus_sign:                                       | JSON-encoded MCP tool result, when reported as a string. |
| `ServerIdentity`                                         | `*string`                                                | :heavy_minus_sign:                                       | Stable server identity inferred by the hook adapter.     |
| `ServerName`                                             | `*string`                                                | :heavy_minus_sign:                                       | Provider-reported MCP server name.                       |
| `URL`                                                    | `*string`                                                | :heavy_minus_sign:                                       | MCP server URL, when available.                          |