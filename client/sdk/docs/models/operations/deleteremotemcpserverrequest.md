# DeleteRemoteMcpServerRequest

## Example Usage

```typescript
import { DeleteRemoteMcpServerRequest } from "@gram/client/models/operations/deleteremotemcpserver.js";

let value: DeleteRemoteMcpServerRequest = {
  id: "<id>",
};
```

## Fields

| Field         | Type     | Required           | Description                               |
| ------------- | -------- | ------------------ | ----------------------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The ID of the remote MCP server to delete |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                            |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                            |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                            |
