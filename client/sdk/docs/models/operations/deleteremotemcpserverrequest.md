# DeleteRemoteMcpServerRequest

## Example Usage

```typescript
import { DeleteRemoteMcpServerRequest } from "@gram/client/models/operations/deleteremotemcpserver.js";

let value: DeleteRemoteMcpServerRequest = {
  id: "<id>",
};
```

## Fields

| Field                                     | Type                                      | Required                                  | Description                               |
| ----------------------------------------- | ----------------------------------------- | ----------------------------------------- | ----------------------------------------- |
| `id`                                      | *string*                                  | :heavy_check_mark:                        | The ID of the remote MCP server to delete |
| `gramSession`                             | *string*                                  | :heavy_minus_sign:                        | Session header                            |
| `gramKey`                                 | *string*                                  | :heavy_minus_sign:                        | API Key header                            |
| `gramProject`                             | *string*                                  | :heavy_minus_sign:                        | project header                            |