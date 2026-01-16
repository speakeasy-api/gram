# AddExternalMCPForm

## Example Usage

```typescript
import { AddExternalMCPForm } from "@gram/client/models/components";

let value: AddExternalMCPForm = {
  name: "My Slack Integration",
  registryId: "ef1d375d-6fc0-4c75-91e1-a03ee8652a8f",
  registryServerSpecifier: "slack",
  slug: "<value>",
  userAgent: "MyApp/1.0",
};
```

## Fields

| Field                                                                                               | Type                                                                                                | Required                                                                                            | Description                                                                                         | Example                                                                                             |
| --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `name`                                                                                              | *string*                                                                                            | :heavy_check_mark:                                                                                  | The display name for the external MCP server.                                                       | My Slack Integration                                                                                |
| `registryId`                                                                                        | *string*                                                                                            | :heavy_check_mark:                                                                                  | The ID of the MCP registry the server is from.                                                      |                                                                                                     |
| `registryServerSpecifier`                                                                           | *string*                                                                                            | :heavy_check_mark:                                                                                  | The canonical server name used to look up the server in the registry (e.g., 'slack', 'ai.exa/exa'). | slack                                                                                               |
| `slug`                                                                                              | *string*                                                                                            | :heavy_check_mark:                                                                                  | A short url-friendly label that uniquely identifies a resource.                                     |                                                                                                     |
| `userAgent`                                                                                         | *string*                                                                                            | :heavy_minus_sign:                                                                                  | Optional custom User-Agent header to send with requests to this MCP server.                         | MyApp/1.0                                                                                           |