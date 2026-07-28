# GetMcpServerRequest

## Example Usage

```typescript
import { GetMcpServerRequest } from "@gram/client/models/operations/getmcpserver.js";

let value: GetMcpServerRequest = {};
```

## Fields

| Field         | Type     | Required           | Description                                             |
| ------------- | -------- | ------------------ | ------------------------------------------------------- |
| `id`          | _string_ | :heavy_minus_sign: | The ID of the MCP server. Mutually exclusive with slug. |
| `slug`        | _string_ | :heavy_minus_sign: | The slug of the MCP server. Mutually exclusive with id. |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                          |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                                          |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                                          |
