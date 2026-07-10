# DeleteMcpServerRequest

## Example Usage

```typescript
import { DeleteMcpServerRequest } from "@gram/client/models/operations/deletemcpserver.js";

let value: DeleteMcpServerRequest = {
  id: "fd6558ba-4e2d-42e8-84c6-44b89ab0f11c",
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `id`                               | *string*                           | :heavy_check_mark:                 | The ID of the MCP server to delete |
| `gramSession`                      | *string*                           | :heavy_minus_sign:                 | Session header                     |
| `gramKey`                          | *string*                           | :heavy_minus_sign:                 | API Key header                     |
| `gramProject`                      | *string*                           | :heavy_minus_sign:                 | project header                     |