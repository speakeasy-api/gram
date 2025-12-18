# ExternalMCPToolDefinition

A proxy tool that references an external MCP server

## Example Usage

```typescript
import { ExternalMCPToolDefinition } from "@gram/client/models/components";

let value: ExternalMCPToolDefinition = {
  createdAt: new Date("2025-07-07T10:46:48.668Z"),
  deploymentExternalMcpId: "<id>",
  deploymentId: "<id>",
  id: "<id>",
  name: "<value>",
  registryId: "<id>",
  remoteUrl: "https://worthless-swath.name/",
  requiresOauth: false,
  slug: "<value>",
  toolUrn: "<value>",
  updatedAt: new Date("2025-03-24T08:29:23.674Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the tool definition was created.                                                         |
| `deploymentExternalMcpId`                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the deployments_external_mcps record                                                |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the deployment                                                                      |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the tool definition                                                                 |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The reverse-DNS name of the external MCP server (e.g., ai.exa/exa)                            |
| `registryId`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the MCP registry                                                                    |
| `remoteUrl`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The URL to connect to the MCP server                                                          |
| `requiresOauth`                                                                               | *boolean*                                                                                     | :heavy_check_mark:                                                                            | Whether the external MCP server requires OAuth authentication                                 |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The slug used for tool prefixing (e.g., github)                                               |
| `toolUrn`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The URN of this tool (tools:externalmcp:<slug>:proxy)                                         |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the tool definition was last updated.                                                    |