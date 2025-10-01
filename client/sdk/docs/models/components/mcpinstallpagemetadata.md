# MCPInstallPageMetadata

Metadata used to configure the MCP install page.

## Example Usage

```typescript
import { MCPInstallPageMetadata } from "@gram/client/models/components";

let value: MCPInstallPageMetadata = {
  createdAt: new Date("2024-12-11T08:56:00.499Z"),
  id: "<id>",
  toolsetId: "4a24e109-85e7-4e32-b029-593d9cc4b165",
  updatedAt: new Date("2025-06-06T05:17:26.574Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the metadata entry was created                                                           |
| `externalDocumentationUrl`                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | A link to external documentation for the MCP install page                                     |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the metadata record                                                                 |
| `logoAssetId`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | The asset ID for the MCP install page logo                                                    |
| `toolsetId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The toolset associated with this install page metadata                                        |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the metadata entry was last updated                                                      |