# McpServer

An MCP server configuration: authentication, environment, and backend selection for an MCP server.

## Example Usage

```typescript
import { McpServer } from "@gram/client/models/components/mcpserver.js";

let value: McpServer = {
  createdAt: new Date("2024-06-05T11:23:32.946Z"),
  id: "9e21a413-68f1-476c-92e1-e21dd5ab1340",
  projectId: "ebaa4ddf-17ca-4cb3-9da8-5fc938e2a349",
  updatedAt: new Date("2024-07-06T16:53:25.627Z"),
  visibility: "public",
};
```

## Fields

| Field                   | Type                                                                                          | Required           | Description                                                                                                 |
| ----------------------- | --------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------------- |
| `createdAt`             | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the MCP server was created                                                                             |
| `environmentId`         | _string_                                                                                      | :heavy_minus_sign: | The ID of the environment associated with the server                                                        |
| `id`                    | _string_                                                                                      | :heavy_check_mark: | The ID of the MCP server                                                                                    |
| `name`                  | _string_                                                                                      | :heavy_minus_sign: | A human-readable display name for the server                                                                |
| `projectId`             | _string_                                                                                      | :heavy_check_mark: | The project ID this MCP server belongs to                                                                   |
| `remoteMcpServerId`     | _string_                                                                                      | :heavy_minus_sign: | The ID of the remote MCP server used as the backend                                                         |
| `slug`                  | _string_                                                                                      | :heavy_minus_sign: | A URL-safe, project-unique slug derived server-side from the name and ID                                    |
| `toolVariationsGroupId` | _string_                                                                                      | :heavy_minus_sign: | The ID of the tool variations group enabling MCP tool filtering for this server, if any.                    |
| `toolsetId`             | _string_                                                                                      | :heavy_minus_sign: | The ID of the toolset used as the backend                                                                   |
| `tunneledMcpServerId`   | _string_                                                                                      | :heavy_minus_sign: | The ID of the tunneled MCP server used as the backend                                                       |
| `updatedAt`             | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the MCP server was last updated                                                                        |
| `userSessionIssuerId`   | _string_                                                                                      | :heavy_minus_sign: | The ID of the user session issuer that gates OAuth-based MCP client authentication for this server, if any. |
| `visibility`            | [components.McpServerVisibility](../../models/components/mcpservervisibility.md)              | :heavy_check_mark: | The visibility of an MCP server                                                                             |
