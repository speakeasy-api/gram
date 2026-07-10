# DeleteMcpEndpointRequest

## Example Usage

```typescript
import { DeleteMcpEndpointRequest } from "@gram/client/models/operations/deletemcpendpoint.js";

let value: DeleteMcpEndpointRequest = {
  id: "0cd13694-cb7b-4a73-ba11-eb9110a3cd1b",
};
```

## Fields

| Field         | Type     | Required           | Description                          |
| ------------- | -------- | ------------------ | ------------------------------------ |
| `id`          | _string_ | :heavy_check_mark: | The ID of the MCP endpoint to delete |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                       |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                       |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                       |
