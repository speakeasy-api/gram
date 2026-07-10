# CustomDomainMcpEndpoint

An MCP endpoint registered under a custom domain, with its parent MCP server and project denormalised for display in the dashboard's delete-impact preview.

## Example Usage

```typescript
import { CustomDomainMcpEndpoint } from "@gram/client/models/components/customdomainmcpendpoint.js";

let value: CustomDomainMcpEndpoint = {
  id: "124040b9-c8a6-4243-9b15-cdb7a613b891",
  mcpServerId: "873b9db1-b75d-4a4b-b0b7-fbaf789dc4bc",
  projectId: "66dd21fa-fbbf-436f-b6e2-31a9789fbc14",
  projectName: "<value>",
  projectSlug: "<value>",
  slug: "<value>",
};
```

## Fields

| Field           | Type     | Required           | Description                                                                                        |
| --------------- | -------- | ------------------ | -------------------------------------------------------------------------------------------------- |
| `id`            | _string_ | :heavy_check_mark: | The ID of the MCP endpoint                                                                         |
| `mcpServerId`   | _string_ | :heavy_check_mark: | The ID of the parent MCP server                                                                    |
| `mcpServerName` | _string_ | :heavy_minus_sign: | The display name of the parent MCP server. May be empty if the parent has no configured name.      |
| `mcpServerSlug` | _string_ | :heavy_minus_sign: | The url-friendly slug of the parent MCP server. May be empty if the parent has no configured slug. |
| `projectId`     | _string_ | :heavy_check_mark: | The ID of the project the endpoint belongs to                                                      |
| `projectName`   | _string_ | :heavy_check_mark: | The display name of the project the endpoint belongs to                                            |
| `projectSlug`   | _string_ | :heavy_check_mark: | The url-friendly slug of the project the endpoint belongs to                                       |
| `slug`          | _string_ | :heavy_check_mark: | The endpoint slug                                                                                  |
