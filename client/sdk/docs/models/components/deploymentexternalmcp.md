# DeploymentExternalMCP

## Example Usage

```typescript
import { DeploymentExternalMCP } from "@gram/client/models/components/deploymentexternalmcp.js";

let value: DeploymentExternalMCP = {
  id: "<id>",
  name: "<value>",
  registryServerSpecifier: "<value>",
  slug: "<value>",
};
```

## Fields

| Field                                 | Type     | Required           | Description                                                           |
| ------------------------------------- | -------- | ------------------ | --------------------------------------------------------------------- |
| `id`                                  | _string_ | :heavy_check_mark: | The ID of the deployment external MCP record.                         |
| `name`                                | _string_ | :heavy_check_mark: | The display name for the external MCP server.                         |
| `organizationMcpCollectionRegistryId` | _string_ | :heavy_minus_sign: | The ID of the internal collection registry the server is from.        |
| `registryId`                          | _string_ | :heavy_minus_sign: | The ID of the external MCP registry the server is from.               |
| `registryServerSpecifier`             | _string_ | :heavy_check_mark: | The canonical server name used to look up the server in the registry. |
| `slug`                                | _string_ | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource.       |
