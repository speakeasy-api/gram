# DeploymentExternalMCP

## Example Usage

```typescript
import { DeploymentExternalMCP } from "@gram/client/models/components";

let value: DeploymentExternalMCP = {
  id: "<id>",
  name: "<value>",
  registryId: "<id>",
  slug: "<value>",
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `id`                                                            | *string*                                                        | :heavy_check_mark:                                              | The ID of the deployment external MCP record.                   |
| `name`                                                          | *string*                                                        | :heavy_check_mark:                                              | The reverse-DNS name of the external MCP server.                |
| `registryId`                                                    | *string*                                                        | :heavy_check_mark:                                              | The ID of the MCP registry the server is from.                  |
| `slug`                                                          | *string*                                                        | :heavy_check_mark:                                              | A short url-friendly label that uniquely identifies a resource. |