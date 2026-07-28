# ListTunneledMcpServerConnectionsRequest

## Example Usage

```typescript
import { ListTunneledMcpServerConnectionsRequest } from "@gram/client/models/operations/listtunneledmcpserverconnections.js";

let value: ListTunneledMcpServerConnectionsRequest = {
  id: "104a957d-e2af-42a5-9f55-c71a59cfa219",
};
```

## Fields

| Field         | Type     | Required           | Description                       |
| ------------- | -------- | ------------------ | --------------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The ID of the tunneled MCP server |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                    |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                    |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                    |
