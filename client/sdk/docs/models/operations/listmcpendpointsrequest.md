# ListMcpEndpointsRequest

## Example Usage

```typescript
import { ListMcpEndpointsRequest } from "@gram/client/models/operations/listmcpendpoints.js";

let value: ListMcpEndpointsRequest = {};
```

## Fields

| Field                                                                   | Type                                                                    | Required                                                                | Description                                                             |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `mcpServerId`                                                           | *string*                                                                | :heavy_minus_sign:                                                      | Optional filter: only return endpoints associated with this MCP server. |
| `gramSession`                                                           | *string*                                                                | :heavy_minus_sign:                                                      | Session header                                                          |
| `gramKey`                                                               | *string*                                                                | :heavy_minus_sign:                                                      | API Key header                                                          |
| `gramProject`                                                           | *string*                                                                | :heavy_minus_sign:                                                      | project header                                                          |