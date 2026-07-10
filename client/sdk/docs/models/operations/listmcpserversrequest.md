# ListMcpServersRequest

## Example Usage

```typescript
import { ListMcpServersRequest } from "@gram/client/models/operations/listmcpservers.js";

let value: ListMcpServersRequest = {};
```

## Fields

| Field                 | Type     | Required           | Description                                              |
| --------------------- | -------- | ------------------ | -------------------------------------------------------- |
| `remoteMcpServerId`   | _string_ | :heavy_minus_sign: | Filter to MCP servers backed by this remote MCP server   |
| `tunneledMcpServerId` | _string_ | :heavy_minus_sign: | Filter to MCP servers backed by this tunneled MCP server |
| `toolsetId`           | _string_ | :heavy_minus_sign: | Filter to MCP servers backed by this toolset             |
| `gramSession`         | _string_ | :heavy_minus_sign: | Session header                                           |
| `gramKey`             | _string_ | :heavy_minus_sign: | API Key header                                           |
| `gramProject`         | _string_ | :heavy_minus_sign: | project header                                           |
