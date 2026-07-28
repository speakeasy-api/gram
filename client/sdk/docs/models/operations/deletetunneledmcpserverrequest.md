# DeleteTunneledMcpServerRequest

## Example Usage

```typescript
import { DeleteTunneledMcpServerRequest } from "@gram/client/models/operations/deletetunneledmcpserver.js";

let value: DeleteTunneledMcpServerRequest = {
  id: "a5a2b11c-d7f7-4032-b6b7-53964747d470",
};
```

## Fields

| Field         | Type     | Required           | Description                                 |
| ------------- | -------- | ------------------ | ------------------------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The ID of the tunneled MCP server to delete |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                              |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                              |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                              |
