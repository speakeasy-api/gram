# ListMcpEndpointsRequest

## Example Usage

```typescript
import { ListMcpEndpointsRequest } from "@gram/client/models/operations/listmcpendpoints.js";

let value: ListMcpEndpointsRequest = {};
```

## Fields

| Field         | Type     | Required           | Description                                                             |
| ------------- | -------- | ------------------ | ----------------------------------------------------------------------- |
| `mcpServerId` | _string_ | :heavy_minus_sign: | Optional filter: only return endpoints associated with this MCP server. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                                          |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                                                          |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                                                          |
