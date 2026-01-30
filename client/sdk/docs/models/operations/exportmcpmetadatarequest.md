# ExportMcpMetadataRequest

## Example Usage

```typescript
import { ExportMcpMetadataRequest } from "@gram/client/models/operations";

let value: ExportMcpMetadataRequest = {
  exportMcpMetadataRequestBody: {
    mcpSlug: "<value>",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                          | *string*                                                                                           | :heavy_minus_sign:                                                                                 | API Key header                                                                                     |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `gramProject`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | project header                                                                                     |
| `exportMcpMetadataRequestBody`                                                                     | [components.ExportMcpMetadataRequestBody](../../models/components/exportmcpmetadatarequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |