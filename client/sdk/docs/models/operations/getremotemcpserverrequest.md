# GetRemoteMcpServerRequest

## Example Usage

```typescript
import { GetRemoteMcpServerRequest } from "@gram/client/models/operations/getremotemcpserver.js";

let value: GetRemoteMcpServerRequest = {};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `id`                                                           | *string*                                                       | :heavy_minus_sign:                                             | The ID of the remote MCP server. Mutually exclusive with slug. |
| `slug`                                                         | *string*                                                       | :heavy_minus_sign:                                             | The slug of the remote MCP server. Mutually exclusive with id. |
| `gramSession`                                                  | *string*                                                       | :heavy_minus_sign:                                             | Session header                                                 |
| `gramKey`                                                      | *string*                                                       | :heavy_minus_sign:                                             | API Key header                                                 |
| `gramProject`                                                  | *string*                                                       | :heavy_minus_sign:                                             | project header                                                 |