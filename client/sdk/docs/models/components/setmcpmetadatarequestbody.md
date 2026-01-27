# SetMcpMetadataRequestBody

## Example Usage

```typescript
import { SetMcpMetadataRequestBody } from "@gram/client/models/components";

let value: SetMcpMetadataRequestBody = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `defaultEnvironmentId`                                                                       | *string*                                                                                     | :heavy_minus_sign:                                                                           | The default environment to load variables from                                               |
| `environmentEntries`                                                                         | [components.McpEnvironmentEntryInput](../../models/components/mcpenvironmententryinput.md)[] | :heavy_minus_sign:                                                                           | The list of environment variables to configure for this MCP                                  |
| `externalDocumentationUrl`                                                                   | *string*                                                                                     | :heavy_minus_sign:                                                                           | A link to external documentation for the MCP install page                                    |
| `instructions`                                                                               | *string*                                                                                     | :heavy_minus_sign:                                                                           | Server instructions returned in the MCP initialize response                                  |
| `logoAssetId`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | The asset ID for the MCP install page logo                                                   |
| `toolsetSlug`                                                                                | *string*                                                                                     | :heavy_check_mark:                                                                           | The slug of the toolset associated with this install page metadata                           |