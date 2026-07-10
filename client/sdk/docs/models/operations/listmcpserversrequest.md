# ListMcpServersRequest

## Example Usage

```typescript
import { ListMcpServersRequest } from "@gram/client/models/operations/listmcpservers.js";

let value: ListMcpServersRequest = {};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `remoteMcpServerId`                                      | *string*                                                 | :heavy_minus_sign:                                       | Filter to MCP servers backed by this remote MCP server   |
| `tunneledMcpServerId`                                    | *string*                                                 | :heavy_minus_sign:                                       | Filter to MCP servers backed by this tunneled MCP server |
| `toolsetId`                                              | *string*                                                 | :heavy_minus_sign:                                       | Filter to MCP servers backed by this toolset             |
| `gramSession`                                            | *string*                                                 | :heavy_minus_sign:                                       | Session header                                           |
| `gramKey`                                                | *string*                                                 | :heavy_minus_sign:                                       | API Key header                                           |
| `gramProject`                                            | *string*                                                 | :heavy_minus_sign:                                       | project header                                           |