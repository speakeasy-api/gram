# SetMcpMetadataRequestBody

## Example Usage

```typescript
import { SetMcpMetadataRequestBody } from "@gram/client/models/components";

let value: SetMcpMetadataRequestBody = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `externalDocumentationUrl`                                         | *string*                                                           | :heavy_minus_sign:                                                 | A link to external documentation for the MCP install page          |
| `instructions`                                                     | *string*                                                           | :heavy_minus_sign:                                                 | Server instructions returned in the MCP initialize response        |
| `logoAssetId`                                                      | *string*                                                           | :heavy_minus_sign:                                                 | The asset ID for the MCP install page logo                         |
| `toolsetSlug`                                                      | *string*                                                           | :heavy_check_mark:                                                 | The slug of the toolset associated with this install page metadata |
| `userAgent`                                                        | *string*                                                           | :heavy_minus_sign:                                                 | Custom User-Agent header for HTTP requests made by this MCP        |