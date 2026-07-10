# McpMetadata

Metadata used to configure the MCP install page. Exactly one of toolset_id or mcp_server_id identifies which backend the metadata belongs to.

## Example Usage

```typescript
import { McpMetadata } from "@gram/client/models/components/mcpmetadata.js";

let value: McpMetadata = {
  createdAt: new Date("2024-09-09T07:11:07.924Z"),
  id: "<id>",
  updatedAt: new Date("2026-08-01T19:33:27.825Z"),
};
```

## Fields

| Field                                                                                          | Type                                                                                           | Required                                                                                       | Description                                                                                    |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)  | :heavy_check_mark:                                                                             | When the metadata entry was created                                                            |
| `defaultEnvironmentId`                                                                         | *string*                                                                                       | :heavy_minus_sign:                                                                             | The default environment to load variables from                                                 |
| `environmentConfigs`                                                                           | [components.McpEnvironmentConfig](../../models/components/mcpenvironmentconfig.md)[]           | :heavy_minus_sign:                                                                             | The list of environment variables configured for this MCP                                      |
| `externalDocumentationText`                                                                    | *string*                                                                                       | :heavy_minus_sign:                                                                             | A blob of text for the button on the MCP server page                                           |
| `externalDocumentationUrl`                                                                     | *string*                                                                                       | :heavy_minus_sign:                                                                             | A link to external documentation for the MCP install page                                      |
| `id`                                                                                           | *string*                                                                                       | :heavy_check_mark:                                                                             | The ID of the metadata record                                                                  |
| `installationOverrideUrl`                                                                      | *string*                                                                                       | :heavy_minus_sign:                                                                             | URL to redirect to instead of showing the default installation page                            |
| `instructions`                                                                                 | *string*                                                                                       | :heavy_minus_sign:                                                                             | Server instructions returned in the MCP initialize response                                    |
| `logoAssetId`                                                                                  | *string*                                                                                       | :heavy_minus_sign:                                                                             | The asset ID for the MCP install page logo                                                     |
| `mcpServerId`                                                                                  | *string*                                                                                       | :heavy_minus_sign:                                                                             | The MCP server associated with this install page metadata. Mutually exclusive with toolset_id. |
| `toolsetId`                                                                                    | *string*                                                                                       | :heavy_minus_sign:                                                                             | The toolset associated with this install page metadata. Mutually exclusive with mcp_server_id. |
| `updatedAt`                                                                                    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)  | :heavy_check_mark:                                                                             | When the metadata entry was last updated                                                       |