# HookMCPData

MCP feature payload.

## Example Usage

```typescript
import { HookMCPData } from "@gram/client/models/components/hookmcpdata.js";

let value: HookMCPData = {};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `command`                                                | *string*                                                 | :heavy_minus_sign:                                       | MCP server command, when available.                      |
| `resultJson`                                             | *string*                                                 | :heavy_minus_sign:                                       | JSON-encoded MCP tool result, when reported as a string. |
| `serverIdentity`                                         | *string*                                                 | :heavy_minus_sign:                                       | Stable server identity inferred by the hook adapter.     |
| `serverName`                                             | *string*                                                 | :heavy_minus_sign:                                       | Provider-reported MCP server name.                       |
| `url`                                                    | *string*                                                 | :heavy_minus_sign:                                       | MCP server URL, when available.                          |