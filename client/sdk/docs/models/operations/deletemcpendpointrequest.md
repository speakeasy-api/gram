# DeleteMcpEndpointRequest

## Example Usage

```typescript
import { DeleteMcpEndpointRequest } from "@gram/client/models/operations/deletemcpendpoint.js";

let value: DeleteMcpEndpointRequest = {
  id: "0cd13694-cb7b-4a73-ba11-eb9110a3cd1b",
};
```

## Fields

| Field                                | Type                                 | Required                             | Description                          |
| ------------------------------------ | ------------------------------------ | ------------------------------------ | ------------------------------------ |
| `id`                                 | *string*                             | :heavy_check_mark:                   | The ID of the MCP endpoint to delete |
| `gramSession`                        | *string*                             | :heavy_minus_sign:                   | Session header                       |
| `gramKey`                            | *string*                             | :heavy_minus_sign:                   | API Key header                       |
| `gramProject`                        | *string*                             | :heavy_minus_sign:                   | project header                       |