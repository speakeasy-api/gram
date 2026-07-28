# SetMcpMetadataRequestBody

## Example Usage

```typescript
import { SetMcpMetadataRequestBody } from "@gram/client/models/components/setmcpmetadatarequestbody.js";

let value: SetMcpMetadataRequestBody = {};
```

## Fields

| Field                       | Type                                                                                           | Required           | Description                                                                                                |
| --------------------------- | ---------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------- |
| `defaultEnvironmentId`      | _string_                                                                                       | :heavy_minus_sign: | The default environment to load variables from. Not supported when mcp_server_id is provided.              |
| `environmentConfigs`        | [components.McpEnvironmentConfigInput](../../models/components/mcpenvironmentconfiginput.md)[] | :heavy_minus_sign: | The list of environment variables to configure for this MCP                                                |
| `externalDocumentationText` | _string_                                                                                       | :heavy_minus_sign: | A blob of text for the button on the MCP server page                                                       |
| `externalDocumentationUrl`  | _string_                                                                                       | :heavy_minus_sign: | A link to external documentation for the MCP install page                                                  |
| `installationOverrideUrl`   | _string_                                                                                       | :heavy_minus_sign: | URL to redirect to instead of showing the default installation page                                        |
| `instructions`              | _string_                                                                                       | :heavy_minus_sign: | Server instructions returned in the MCP initialize response                                                |
| `logoAssetId`               | _string_                                                                                       | :heavy_minus_sign: | The asset ID for the MCP install page logo                                                                 |
| `mcpServerId`               | _string_                                                                                       | :heavy_minus_sign: | The ID of the MCP server associated with this install page metadata. Mutually exclusive with toolset_slug. |
| `toolsetSlug`               | _string_                                                                                       | :heavy_minus_sign: | The slug of the toolset associated with this install page metadata. Mutually exclusive with mcp_server_id. |
