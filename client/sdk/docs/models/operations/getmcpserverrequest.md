# GetMcpServerRequest

## Example Usage

```typescript
import { GetMcpServerRequest } from "@gram/client/models/operations/getmcpserver.js";

let value: GetMcpServerRequest = {};
```

## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `id`                                                    | *string*                                                | :heavy_minus_sign:                                      | The ID of the MCP server. Mutually exclusive with slug. |
| `slug`                                                  | *string*                                                | :heavy_minus_sign:                                      | The slug of the MCP server. Mutually exclusive with id. |
| `gramSession`                                           | *string*                                                | :heavy_minus_sign:                                      | Session header                                          |
| `gramKey`                                               | *string*                                                | :heavy_minus_sign:                                      | API Key header                                          |
| `gramProject`                                           | *string*                                                | :heavy_minus_sign:                                      | project header                                          |