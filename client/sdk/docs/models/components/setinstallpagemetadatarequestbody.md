# SetInstallPageMetadataRequestBody

## Example Usage

```typescript
import { SetInstallPageMetadataRequestBody } from "@gram/client/models/components";

let value: SetInstallPageMetadataRequestBody = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `externalDocumentationUrl`                                         | *string*                                                           | :heavy_minus_sign:                                                 | A link to external documentation for the MCP install page          |
| `logoAssetId`                                                      | *string*                                                           | :heavy_minus_sign:                                                 | The asset ID for the MCP install page logo                         |
| `toolsetSlug`                                                      | *string*                                                           | :heavy_check_mark:                                                 | The slug of the toolset associated with this install page metadata |