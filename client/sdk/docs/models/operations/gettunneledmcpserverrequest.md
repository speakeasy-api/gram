# GetTunneledMcpServerRequest

## Example Usage

```typescript
import { GetTunneledMcpServerRequest } from "@gram/client/models/operations/gettunneledmcpserver.js";

let value: GetTunneledMcpServerRequest = {
  id: "19dae3db-b861-4ce8-a10c-9b7909fe0588",
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `id`                              | *string*                          | :heavy_check_mark:                | The ID of the tunneled MCP server |
| `gramSession`                     | *string*                          | :heavy_minus_sign:                | Session header                    |
| `gramKey`                         | *string*                          | :heavy_minus_sign:                | API Key header                    |
| `gramProject`                     | *string*                          | :heavy_minus_sign:                | project header                    |