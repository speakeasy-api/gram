# AddExternalMCPForm

## Example Usage

```typescript
import { AddExternalMCPForm } from "@gram/client/models/components/addexternalmcpform.js";

let value: AddExternalMCPForm = {
  name: "My Slack Integration",
  registryServerSpecifier: "slack",
  selectedRemotes: ["https://mcp.example.com/sse"],
  slug: "<value>",
};
```

## Fields

| Field                                 | Type       | Required           | Description                                                                                                                       | Example                                   |
| ------------------------------------- | ---------- | ------------------ | --------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------- |
| `name`                                | _string_   | :heavy_check_mark: | The display name for the external MCP server.                                                                                     | My Slack Integration                      |
| `organizationMcpCollectionRegistryId` | _string_   | :heavy_minus_sign: | The ID of the internal collection registry the server is from.                                                                    |                                           |
| `registryId`                          | _string_   | :heavy_minus_sign: | The ID of the external MCP registry the server is from.                                                                           |                                           |
| `registryServerSpecifier`             | _string_   | :heavy_check_mark: | The canonical server name used to look up the server in the registry (e.g., 'slack', 'ai.exa/exa').                               | slack                                     |
| `selectedRemotes`                     | _string_[] | :heavy_minus_sign: | URLs of the remotes to use for this MCP server. If not provided, the backend will auto-select based on transport type preference. | [<br/>"https://mcp.example.com/sse"<br/>] |
| `slug`                                | _string_   | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource.                                                                   |                                           |
