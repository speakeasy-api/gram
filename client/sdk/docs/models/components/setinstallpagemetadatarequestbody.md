# SetInstallPageMetadataRequestBody

## Example Usage

```typescript
import { SetInstallPageMetadataRequestBody } from "@gram/client/models/components";

let value: SetInstallPageMetadataRequestBody = {
  toolsetId: "80cebb6c-0232-4c06-a6c2-8c528ee56821",
};
```

## Fields

| Field                                                     | Type                                                      | Required                                                  | Description                                               |
| --------------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------- |
| `externalDocumentationUrl`                                | *string*                                                  | :heavy_minus_sign:                                        | A link to external documentation for the MCP install page |
| `logoAssetId`                                             | *string*                                                  | :heavy_minus_sign:                                        | The asset ID for the MCP install page logo                |
| `toolsetId`                                               | *string*                                                  | :heavy_check_mark:                                        | The toolset associated with this install page metadata    |