# McpMetadata

Metadata used to configure the MCP install page.

## Example Usage

```typescript
import { McpMetadata } from "@gram/client/models/components";

let value: McpMetadata = {
  createdAt: new Date("2023-09-10T07:11:07.924Z"),
  id: "<id>",
  toolsetId: "d6857a14-9fe0-4abf-b801-5641a43197a3",
  updatedAt: new Date("2024-07-05T03:59:55.214Z"),
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